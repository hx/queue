package queue

import "time"

// A job to be run by a Queue.
type Job struct {
	// Give jobs a key to ensure no more than one are queued at once.
	Key string

	// Wait this long before running the job. Combine with Key to debounce jobs.
	Delay time.Duration

	// Repeat job this long after it completes.
	Repeat time.Duration

	// The work to be done by the job.
	Perform func()

	// If true, don't wait for previous job with same key to finish.
	Simultaneous bool

	// Optional function that, given a list of keys of running jobs, returns true if there is a conflict with another job.
	// Test is performed when job is about to run, not when it is queued.
	IsConflict func([]string) bool

	// When false, job will wait for conflicts to be resolved. When true, job will be discarded immediately on conflict.
	// Ignored if no IsConflict function is set.
	DiscardOnConflict bool

	runAt *time.Time
}

// Add the job to a queue.
func (j *Job) AddTo(q *Queue) {
	q.Add(j)
}

func (j *Job) hasConflict(runningKeys *[]string) bool {
	if j.IsConflict == nil {
		return false
	}
	return j.IsConflict(*runningKeys)
}
