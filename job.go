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

	runAt *time.Time
}

// Add the job to a queue.
func (j *Job) AddTo(q *Queue) {
	q.Add(j)
}
