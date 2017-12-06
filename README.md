# Queue

Job queue for Go.

Available behaviours:

- Specify how many worker processes should perform jobs
- Run jobs with a delay
- Repeat (loop) jobs after a delay
- Debounce jobs
- Prevent jobs from running simultaneously
- Prevent conflicts between different jobs
- Cancel jobs that haven't run yet
- Run jobs inline

### Example

```go
package main

import (
	"github.com/hx/queue"
	"fmt"
	"time"
)

q := queue.NewQueue()

func sayHello() {
	q.Add(&queue.Job{
		Key:     "hello",
		Delay:   10 * time.Millisecond,
		Perform: func() { fmt.Println("Hello!") },
	})
}

sayHello() // Schedule a job
sayHello() // Replaces the first job

// => "Hello!"
```
