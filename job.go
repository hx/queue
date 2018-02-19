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
	// Test is performed when job is about to run, not when it is queued. Use this instead of HasConflict when you need
	// to consider all running jobs, e.g. enforce a maximum of 5 running jobs with keys starting with "abc-".
	HasConflicts func([]string) bool

	// Optional function that, given a key of a running job, returns true if it conflicts with this job. Test is
	// performed when job is about to run, not when it is queued. Use this instead of HasConflicts if you can test each
	// key separately.
	HasConflict func(string) bool

	// When false, job will wait for conflicts to be resolved. When true, job will be discarded immediately on conflict.
	// Ignored if no HasConflict or HasConflicts functions are set.
	DiscardOnConflict bool

	// Optional function that, given the key of another queued job, returns true if this job can take the other job's
	// place. Can be used, for example, to replace a queued sync-one job with a sync-all job.
	CanReplace func(string) bool

	runAt *time.Time
}

// Add the job to a queue.
func (j *Job) AddTo(q *Queue) {
	q.Add(j)
}

func (j *Job) hasConflict(runningKeys []string) bool {
	if j.HasConflict != nil {
		for _, key := range runningKeys {
			if j.HasConflict(key) {
				return true
			}
		}
	}
	if j.HasConflicts != nil {
		return j.HasConflicts(runningKeys)
	}
	return false
}
