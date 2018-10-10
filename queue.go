package queue

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// A queue that contains any number of workers and queued jobs.
type Queue struct {
	// When true, all jobs will be run once, and immediately. Delay and Repeat properties are ignored.
	Inline bool

	// Function to be called when a job panics. Ignored on inline queues. If absent, panicking jobs will fail silently.
	OnPanic func(*Job, interface{})

	sync    sync.Mutex
	state   uint32
	stream  chan chan func()
	waiting *jobHeap
	running map[string]int
	active  sync.WaitGroup
	autoKey uint64
}

type state uint32

const (
	stateUnused state = iota
	stateStarted
	stateStopped
	statePaused
)

// Wait for running jobs to complete. Does not wait for delayed jobs.
func (q *Queue) Wait() *Queue {
	if q.didStart() {
		q.active.Wait()
	}
	return q
}

// Wait for all jobs to complete, including delayed jobs.
func (q *Queue) WaitAll() *Queue {
	if q.didStart() {
		q.waiting.wait()
	}
	return q.Wait()
}

// Clear the queue, and wait for existing jobs to finish. Repeat jobs will not be repeated.
func (q *Queue) Shutdown() *Queue {
	q.Clear()
	q.interruptAndWait(func() {
		q.setState(stateStopped)
		q.waiting.clear()
	})
	return q.Wait()
}

// Add a job to the queue.
func (q *Queue) Add(j *Job) *Queue {
	if q.Inline {
		q.performInline(j)
	} else {
		q.interruptAndWait(func() { q.enqueue(j) })
	}
	return q
}

// Remove jobs from the queue. Does not remove running jobs. Returns the number of jobs removed.
func (q *Queue) Remove(keys ...string) (count uint) {
	q.interruptAndWait(func() {
		for _, key := range keys {
			if q.waiting.remove(key) {
				count++
			}
		}
	})
	return
}

// Clear the queue, and return the jobs that were cancelled. Does not affect running jobs.
func (q *Queue) Clear() (jobs []*Job) {
	q.interruptAndWait(func() { jobs = q.waiting.clear() })
	return
}

// Pause queue processing. Does not affect running jobs.
func (q *Queue) Pause() *Queue {
	q.interruptAndWait(func() { q.setState(statePaused) })
	return q
}

// Resume queue processing after having been Paused.
func (q *Queue) Resume() *Queue {
	q.interruptAndWait(func() { q.setState(stateStarted) })
	return q
}

// Add an anonymous function to the queue, to be performed immediately.
func (q *Queue) AddFunc(key string, f func()) *Queue {
	return q.Add(&Job{Perform: f, Key: key})
}

// Force the next job in the queue to be performed synchronously, and return the job. Nil will be returned if no job is
// waiting. No jobs can be queued until the forced job completes. Repeat jobs will not be re-queued. OnPanic is ignored.
//
// This method is intended for testing your Queue consumption.
func (q *Queue) Force() (job *Job) {
	q.interruptAndWait(func() {
		if len(q.waiting.jobs) != 0 {
			job = q.waiting.jobs[0]
			q.waiting.remove(job.Key)
			job.Perform()
		}
	})
	return
}

// Force all jobs in the queue to be performed, in sequence, and return the performed jobs in the order they were run.
// No jobs can be queued until all jobs complete. Repeat jobs will not be re-queued.
//
// This method is intended for testing your Queue consumption.
func (q *Queue) Drain() (jobs []*Job) {
	q.interruptAndWait(func() {
		jobs = q.waiting.clear()
		for _, j := range jobs {
			j.Perform()
		}
	})
	return
}

// Get a list of all queued jobs, in the order they are due to be performed (not considering conflicts).
//
// This method is intended for testing your Queue consumption.
func (q *Queue) Waiting() (jobs []*Job) {
	q.interruptAndWait(func() { jobs = q.waiting.jobs })
	return
}

func (q *Queue) getState() state {
	s := state(atomic.LoadUint32(&q.state))
	return s
}

func (q *Queue) setState(s state) {
	atomic.StoreUint32(&q.state, uint32(s))
}

func (q *Queue) swapState(old, new state) bool {
	return atomic.CompareAndSwapUint32(&q.state, uint32(old), uint32(new))
}

func (q *Queue) start() {
	q.sync.Lock()
	if q.swapState(stateUnused, stateStarted) {
		q.stream = make(chan chan func())
		q.waiting = new(jobHeap)
		q.running = make(map[string]int)
		go q.run()
	}
	q.sync.Unlock()
}

func (q *Queue) didStart() bool {
	return q.getState() != stateUnused
}

func (q *Queue) restart() (ret chan func()) {
	q.sync.Lock()
	ret, ok := <-q.stream
	if !ok {
		q.stream = make(chan chan func())
		q.swapState(stateStopped, stateStarted)
		go q.run()
		ret = <-q.stream
	}
	q.sync.Unlock()
	return
}

func (q *Queue) run() {
	var (
		interrupt = make(chan func())
		firstRun  = true
	)
	for {
		var (
			now         = time.Now()
			timeout     <-chan time.Time
			runningKeys []string
			unqueue     []*Job
		)
		if q.getState() != statePaused {
			for _, job := range q.waiting.jobs {
				if job.runAt.After(now) {
					timeout = time.After(job.runAt.Sub(now))
					break
				}
				if job.Simultaneous || q.running[job.Key] == 0 {
					if runningKeys == nil {
						runningKeys = make([]string, 0, len(q.running))
						for jobKey := range q.running {
							runningKeys = append(runningKeys, jobKey)
						}
					}
					if job.hasConflict(runningKeys) {
						if job.DiscardOnConflict {
							unqueue = append(unqueue, job)
						}
					} else {
						unqueue = append(unqueue, job)
						q.running[job.Key]++
						q.active.Add(1)
						go q.perform(job)
					}
				}
			}
			for _, job := range unqueue {
				q.waiting.remove(job.Key)
			}
		}
		if !firstRun && timeout == nil && len(q.waiting.jobs) == 0 && len(q.running) == 0 {
			close(q.stream)
			return
		}
		firstRun = false
		if timeout == nil {
			q.stream <- interrupt
			(<-interrupt)()
		} else {
			select {
			case <-timeout:
			case q.stream <- interrupt:
				(<-interrupt)()
			}
		}
	}
}

func (q *Queue) interrupt(fn func()) {
	q.start()
	ret, ok := <-q.stream
	if !ok {
		ret = q.restart()
	}
	ret <- fn
}

func (q *Queue) interruptAndWait(fn func()) {
	lock := sync.Mutex{}
	lock.Lock()
	q.interrupt(func() {
		fn()
		lock.Unlock()
	})
	lock.Lock()
}

func (q *Queue) enqueue(j *Job) {
	runAt := time.Now().Add(j.Delay)
	j.runAt = &runAt
	if j.Key == "" {
		q.autoKey++
		j.Key = "Anonymous job #" + strconv.FormatUint(q.autoKey, 10)
	}
	if j.CanReplace != nil {
		var remove []string
		for jobKey := range q.waiting.keys {
			if j.CanReplace(jobKey) {
				remove = append(remove, jobKey)
			}
		}
		for _, jobKey := range remove {
			q.waiting.remove(jobKey)
		}
	}
	q.waiting.add(j)
}

func (q *Queue) perform(j *Job) {
	defer func() {
		if err := recover(); err != nil && q.OnPanic != nil {
			q.OnPanic(j, err)
		}
		q.interrupt(func() {
			if count := q.running[j.Key] - 1; count == 0 {
				delete(q.running, j.Key)
			} else {
				q.running[j.Key] = count
			}
			if j.Repeat > 0 && q.getState() != stateStopped {
				j.Delay = j.Repeat
				q.enqueue(j)
			}
			q.active.Done()
		})
	}()
	j.Perform()
}

func (q *Queue) performInline(j *Job) {
	defer func() {
		if err := recover(); err != nil && q.OnPanic != nil {
			q.OnPanic(j, err)
		}
	}()
	j.Perform()
}
