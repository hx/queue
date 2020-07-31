package queue

// Panic represents a panic from a job that was recovered by a Queue.
type Panic struct {
	// The Job that panicked.
	Job *Job

	// The error value that was passed to the call of panic.
	Error interface{}

	// The stack trace at the point the panic was recovered.
	FormattedStack []byte
}
