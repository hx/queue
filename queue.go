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
					q.perform(job, false)
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
	q.initialise()
	return q.add(job)
}

func (q *Queue) add(job *Job) *Queue {
	runAt := time.Now().Add(job.Delay)
	if q.Inline {
		job.Perform()
	} else {
		q.enqueue.Lock()
		job.runAt = &runAt
		if job.Key == "" {
			q.autoKey++
			job.Key = "__anonymous__job__" + strconv.FormatUint(q.autoKey, 16)
		}
		if job.CanReplace != nil {
			for key := range q.waiting.keys {
				if job.CanReplace(key) {
					q.waiting.remove(key)
				}
			}
		}
		q.waiting.add(job)
		q.enqueue.Unlock()
		if len(q.workers) > 0 {
			q.seeker <- struct{}{}
		}
	}
	return q
}

// Remove jobs from the queue. Does not remove running jobs. Returns the number of jobs removed.
func (q *Queue) Remove(keys ...string) (count uint) {
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	for _, key := range keys {
		if q.waiting.remove(key) {
			count++
		}
	}
	return
}

// Clear the queue, and return the jobs that were cancelled. Does not affect running jobs.
func (q *Queue) Clear() (jobs []*Job) {
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	jobs = q.waiting.jobs
	for _, job := range jobs {
		q.waiting.remove(job.Key)
	}
	return
}

// Add an anonymous function to the queue, to be performed immediately.
func (q *Queue) AddFunc(key string, f func()) *Queue {
	return q.Add(&Job{Perform: f, Key: key})
}

// Force the next job in the queue to be performed synchronously, and return the job. Nil will be returned if no job is
// waiting. No jobs can be queued until the forced job completes. Repeat jobs will not be re-queued.
//
// This method is intended for testing your Queue consumption.
func (q *Queue) Force() (job *Job) {
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	if len(q.waiting.jobs) > 0 {
		job = q.waiting.jobs[0]
		q.perform(job, true)
		q.waiting.remove(job.Key)
	}
	return
}

// Force all jobs in the queue to be performed, in sequence, and return the performed jobs in the order they were run.
// No jobs can be queued until all jobs complete. Repeat jobs will not be re-queued.
//
// This method is intended for testing your Queue consumption.
func (q *Queue) Drain() (jobs []*Job) {
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	jobs = q.waiting.jobs
	for _, job := range jobs {
		q.perform(job, true)
		q.waiting.remove(job.Key)
	}
	return
}

// Get a list of all queued jobs, in the order they are due to be performed (not considering conflicts).
//
// This method is intended for testing your Queue consumption.
func (q *Queue) Waiting() []*Job {
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	return q.waiting.jobs
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
	var runningKeys []string
	for _, job := range q.waiting.jobs {
		if job == nil {
			return nil
		}
		if job.runAt.After(now) {
			return job.runAt
		}
		if job.Simultaneous || q.running[job.Key] == 0 {
			if runningKeys == nil {
				runningKeys = make([]string, 0, len(q.running))
				for k := range q.running {
					runningKeys = append(runningKeys, k)
				}
			}
			if job.hasConflict(runningKeys) {
				if job.DiscardOnConflict {
					q.waiting.remove(job.Key)
				}
			} else {
				q.active.Add(1)
				q.waiting.remove(job.Key)
				q.running[job.Key] += 1
				q.outbox <- job
			}
		}
	}
	return nil
}

func (q *Queue) perform(job *Job, forced bool) {
	defer func() {
		if err := recover(); err != nil && q.OnPanic != nil {
			q.OnPanic(job, err)
		}
		if !forced {
			go q.complete(job)
		}
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
			q.add(job)
		} else {
			q.seeker <- struct{}{}
		}
	}
}
