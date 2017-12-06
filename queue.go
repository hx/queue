package queue

import (
	"strconv"
	"sync"
	"time"
)

// A queue that contains any number of workers and queued jobs.
type Queue struct {
	// When true, all jobs will be run once, and immediately. Delay and Repeat properties are ignored.
	Inline bool

	// Function to be called when a job panics. Ignored on inline queues. If absent, panicking jobs will fail silently.
	OnPanic func(*Job, interface{})

	waiting *jobHeap
	running map[string]int
	inbox   chan *Job
	outbox  chan *Job
	manage  sync.Mutex
	enqueue sync.Mutex
	workers []chan struct{}
	seeker  chan struct{}
	active  sync.WaitGroup
	autoKey uint64
}

// Return a new queue with one running worker.
func NewQueue() *Queue {
	return (&Queue{}).Work(1)
}

// Set the number of workers processing a queue. If the given number is less than the current number of workers,
// existing workers will be allowed to finish their current job before being terminated.
func (q *Queue) Work(workers int) *Queue {
	q.initialise()
	q.manage.Lock()
	defer q.manage.Unlock()
	if workers < 0 {
		workers = len(q.workers) + workers
	}
	if workers < 0 {
		workers = 0
	}
	for workers > len(q.workers) {
		if len(q.workers) == 0 {
			q.seek()
		}
		cancel := make(chan struct{}, 1) // Buffer allows jobs to cancel their own workers
		q.workers = append(q.workers, cancel)
		go func() {
			for {
				select {
				case job := <-q.outbox:
					q.perform(job)
				case <-cancel:
					break
				}
			}
		}()
	}
	for workers < len(q.workers) {
		cancel := q.workers[0]
		q.workers = q.workers[1:]
		cancel <- struct{}{}
		if len(q.workers) == 0 {
			close(q.seeker)
		}
	}
	return q
}

// Get the number of running workers.
func (q *Queue) Workers() int {
	q.manage.Lock()
	defer q.manage.Unlock()
	if q.workers == nil {
		return 0
	} else {
		return len(q.workers)
	}
}

// Wait for running jobs to complete. Does not wait for delayed jobs.
func (q *Queue) Wait() *Queue {
	q.active.Wait()
	return q
}

// Wait for all jobs to complete, including delayed jobs.
func (q *Queue) WaitAll() *Queue {
	q.waiting.wait()
	return q.Wait()
}

// Stop all running workers.
func (q *Queue) StopAll() *Queue {
	return q.Work(0)
}

// Wait for existing jobs to finish, and stop all workers.
func (q *Queue) Shutdown() *Queue {
	return q.StopAll().Wait()
}

// Add a job to the queue.
func (q *Queue) Add(job *Job) *Queue {
	runAt := time.Now().Add(job.Delay)
	if q.Inline {
		job.Perform()
	} else {
		if q.workers == nil || len(q.workers) == 0 { // TODO fix races
			panic("queue has no workers")
		}
		q.enqueue.Lock()
		job.runAt = &runAt
		if job.Key == "" {
			q.autoKey++
			job.Key = "__anonymous__job__" + strconv.FormatUint(q.autoKey, 16)
		}
		q.waiting.add(job)
		q.enqueue.Unlock()
		q.seeker <- struct{}{}
	}
	return q
}

// Add an anonymous function to the queue, to be performed immediately.
func (q *Queue) AddFunc(key string, f func()) *Queue {
	return q.Add(&Job{Perform: f, Key: key})
}

func (q *Queue) initialise() {
	q.manage.Lock()
	defer q.manage.Unlock()
	if q.workers == nil {
		q.waiting = &jobHeap{}
		q.running = make(map[string]int)
		q.workers = []chan struct{}{}
		q.inbox = make(chan *Job, 100)
		q.outbox = make(chan *Job, 100)
	}
}

func (q *Queue) seek() {
	q.seeker = make(chan struct{})
	go func() {
		var schedule *time.Time
		for {
			now := time.Now()
			open := true
			if schedule != nil && schedule.After(now) {
				select {
				case _, open = <-q.seeker:
				case <-time.After(schedule.Sub(now)):
				}
			} else {
				_, open = <-q.seeker
			}
			schedule = q.next()
			if !open {
				break
			}
		}
		q.seeker = nil
	}()
}

func (q *Queue) next() *time.Time {
	now := time.Now()
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	var runningKeys *[]string
	for _, job := range q.waiting.jobs {
		if job == nil {
			return nil
		}
		if job.runAt.After(now) {
			return job.runAt
		}
		if job.Simultaneous || q.running[job.Key] == 0 {
			if runningKeys == nil {
				keys := make([]string, 0, len(q.running))
				for k := range q.running {
					keys = append(keys, k)
				}
				runningKeys = &keys
			}
			if job.hasConflict(runningKeys) {
				if job.DiscardOnConflict {
					q.waiting.remove(job)
				}
			} else {
				q.active.Add(1)
				q.waiting.remove(job)
				q.running[job.Key] += 1
				q.outbox <- job
			}
		}
	}
	return nil
}

func (q *Queue) perform(job *Job) {
	defer func() {
		if err := recover(); err != nil && q.OnPanic != nil {
			q.OnPanic(job, err)
		}
		go q.complete(job)
	}()
	job.Perform()
}

func (q *Queue) complete(job *Job) {
	q.enqueue.Lock()
	if count := q.running[job.Key] - 1; count == 0 {
		delete(q.running, job.Key)
	} else {
		q.running[job.Key] = count
	}
	q.enqueue.Unlock()
	q.active.Done()
	q.manage.Lock()
	defer q.manage.Unlock()
	if len(q.workers) > 0 {
		if job.Repeat > 0 {
			job.Delay = job.Repeat
			q.Add(job)
		} else {
			q.seeker <- struct{}{}
		}
	}
}
