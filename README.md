# Queue

[![GoDoc](https://godoc.org/github.com/hx/queue?status.svg)](https://godoc.org/github.com/hx/queue)

Job queue for Go.

Available behaviours:

- Run jobs with a delay
- Repeat (loop) jobs after a delay
- Debounce jobs
- Prevent jobs from running simultaneously
- Prevent conflicts between different jobs
- Replace jobs with other jobs
- Cancel jobs that haven't run yet
- Run jobs inline
- Pause/resume processing

### Example

```go
package main

import (
    "fmt"
    "github.com/hx/queue"
    "time"
)

func sayHello() *queue.Job {
    return &queue.Job{
        Key:     "hello",
        Delay:   10 * time.Millisecond,
        Perform: func() { fmt.Println("Hello!") },
    }
}

func main() {
    q := new(queue.Queue)
    q.Add(sayHello()) // Schedule a job
    q.Add(sayHello()) // Replaces the first job
}

// => "Hello!"
```
