package queue_test

import (
	"github.com/hx/queue"
	"reflect"
	"sync"
	"testing"
	"time"
)

func Assert(tb testing.TB, cond bool, msg string, v ...interface{}) {
	tb.Helper()
	if !cond {
		tb.Logf(msg, v...)
		tb.FailNow()
	}
}

func Equal(tb testing.TB, exp interface{}, act interface{}) {
	tb.Helper()
	Assert(tb, reflect.DeepEqual(exp, act), "Expected %v to be equal to %v", act, exp)
}

func NotEqual(tb testing.TB, exp interface{}, act interface{}) {
	tb.Helper()
	Assert(tb, !reflect.DeepEqual(exp, act), "Expected %v not to be equal to %v", act, exp)
}

func TestQueue_Inline(t *testing.T) {
	run := false
	q := queue.Queue{Inline: true}
	q.Add(&queue.Job{Perform: func() { run = true }})
	Assert(t, run, "Job should run inline")
}

func TestQueue_Add(t *testing.T) {
	run := false
	(&queue.Queue{}).
		Work(1).
		AddFunc("", func() { run = true }).
		WaitAll()
	Assert(t, run, "Job should have run")
}

func TestQueue_RunDelayed(t *testing.T) {
	seq := ""
	(&queue.Queue{}).
		Work(1).
		Add(&queue.Job{Key: "a", Perform: func() { seq = seq + "a" }, Delay: 40 * time.Millisecond}).
		Add(&queue.Job{Key: "b", Perform: func() { seq = seq + "b" }, Delay: 20 * time.Millisecond}).
		Add(&queue.Job{Key: "c", Perform: func() { seq = seq + "c" }, Delay: 60 * time.Millisecond}).
		WaitAll()
	Equal(t, "bac", seq)
}

func TestQueue_Override(t *testing.T) {
	seq := ""
	(&queue.Queue{}).
		Work(1).
		Add(&queue.Job{Key: "a", Perform: func() { seq = seq + "a" }, Delay: 40 * time.Millisecond}).
		Add(&queue.Job{Key: "a", Perform: func() { seq = seq + "b" }, Delay: 20 * time.Millisecond}).
		WaitAll()
	Equal(t, "b", seq)
}

func TestQueue_AutoKey(t *testing.T) {
	j1 := queue.Job{Perform: func() {}}
	j2 := queue.Job{Perform: func() {}}
	Equal(t, j1.Key, j2.Key)
	queue.NewQueue().Add(&j1).Add(&j2)
	NotEqual(t, j1.Key, j2.Key)
}

func TestQueue_Wait(t *testing.T) {
	q := queue.NewQueue()
	q.Wait()
	q.WaitAll()
	q.StopAll().WaitAll()
}

func TestQueue_Repetition(t *testing.T) {
	count := 0
	done := make(chan struct{})
	q := queue.NewQueue()
	q.Add(&queue.Job{Repeat: 10 * time.Millisecond, Perform: func() {
		count++
		if count == 4 {
			q.StopAll()
			done <- struct{}{}
		}
	}})
	<-done
	Equal(t, 4, count)
}

func TestQueue_Debounce(t *testing.T) {
	count := 0
	job := &queue.Job{
		Key:     "do me!",
		Perform: func() { count++ },
		Delay:   10 * time.Millisecond,
	}
	q := queue.NewQueue()
	q.Add(job)
	time.Sleep(2 * time.Millisecond)
	q.Add(job)
	time.Sleep(20 * time.Millisecond)
	q.Add(job)
	time.Sleep(2 * time.Millisecond)
	q.Add(job)
	q.WaitAll()
	Equal(t, 2, count)
}

func TestQueue_OnPanic(t *testing.T) {
	var caught interface{}
	(&queue.Queue{
		OnPanic: func(j *queue.Job, err interface{}) {
			caught = err
		},
	}).
		Work(1).
		AddFunc("", func() { panic("abc") }).
		WaitAll()
	Equal(t, "abc", caught)
}

func TestQueue_Exclusive(t *testing.T) {
	var (
		seq  = ""
		lock = sync.Mutex{}
		q    = queue.NewQueue().Work(3)
		add  = func(s string) {
			lock.Lock()
			seq += s
			lock.Unlock()
		}
		job = &queue.Job{
			Key: "stuff",
			Perform: func() {
				add("a")
				time.Sleep(20 * time.Millisecond)
				add("z")
			},
		}
	)
	q.Add(job)
	time.Sleep(10 * time.Millisecond)
	q.Add(job).WaitAll()
	job.Simultaneous = true
	q.Add(job)
	time.Sleep(10 * time.Millisecond)
	q.Add(job).WaitAll()
	Equal(t, "azazaazz", seq)
}

func TestQueue_Conflicts(t *testing.T) {
	q := queue.NewQueue().Work(3)
	count := 0
	q.Add(&queue.Job{
		HasConflicts:      func(_ []string) bool { return true },
		DiscardOnConflict: true,
		Perform:           func() { count++ },
	}).WaitAll()
	Equal(t, 0, count)

	count = 0
	q.AddFunc("", func() {
		time.Sleep(50 * time.Millisecond)
		count = 5
	})
	time.Sleep(20 * time.Millisecond)
	q.Add(&queue.Job{
		HasConflicts: func(k []string) bool { return len(k) > 0 },
		Perform:      func() { count *= 2 },
	}).WaitAll()
	Equal(t, 10, count)
}

func TestQueue_Conflict(t *testing.T) {
	var (
		count  = 0
		tested []string
	)
	queue.
		NewQueue().
		Add(&queue.Job{
			Key: "thorn",
			Perform: func() {
				time.Sleep(50 * time.Millisecond)
				count += 1
			},
		}).
		Add(&queue.Job{
			Delay: 20 * time.Millisecond,
			HasConflict: func(key string) bool {
				tested = append(tested, key)
				return true
			},
			Perform:           func() { count += 1 },
			DiscardOnConflict: true,
		}).WaitAll()
	Equal(t, []string{"thorn"}, tested)
	Equal(t, 1, count)
}

func TestQueue_Remove(t *testing.T) {
	q := queue.NewQueue().Add(&queue.Job{
		Key:     "fail",
		Delay:   50 * time.Millisecond,
		Perform: func() { t.FailNow() },
	})
	Equal(t, uint(1), q.Remove("fail"))
	q.WaitAll()
}

func TestQueue_CanReplace(t *testing.T) {
	var (
		seq  = ""
		lock = sync.Mutex{}
		add  = func(s string) {
			lock.Lock()
			seq += s
			lock.Unlock()
		}
	)
	queue.
		NewQueue().
		Work(3).
		Add(&queue.Job{
			Key:     "a",
			Delay:   10 * time.Millisecond,
			Perform: func() { add("a") },
		}).
		Add(&queue.Job{
			Key:     "b",
			Delay:   20 * time.Millisecond,
			Perform: func() { add("b") },
		}).
		Add(&queue.Job{
			Key:        "c",
			Delay:      10 * time.Millisecond,
			Perform:    func() { add("c") },
			CanReplace: func(key string) bool { return key == "a" },
		}).
		WaitAll()
	Equal(t, "cb", seq)
}

func TestQueue_Clear(t *testing.T) {
	q := &queue.Queue{}
	q.
		AddFunc("foo", func() {}).
		AddFunc("baz", func() {}).
		AddFunc("bar", func() {})
	Equal(t, 3, len(q.Waiting()))
	Equal(t, 3, len(q.Clear()))
	Equal(t, 0, len(q.Waiting()))
}

func TestQueue_Force(t *testing.T) {
	var (
		run bool
		q   = &queue.Queue{}
	)
	q.Add(&queue.Job{
		Key:     "2",
		Perform: func() { run = true },
		Delay:   1 * time.Second,
	}).Add(&queue.Job{
		Key:     "1",
		Perform: func() {},
	})
	Equal(t, 2, len(q.Waiting()))
	Equal(t, "1", q.Force().Key)
	Equal(t, 1, len(q.Waiting()))
	Assert(t, !run, "")
	Equal(t, "2", q.Force().Key)
	Equal(t, 0, len(q.Waiting()))
	Assert(t, run, "")
}

func TestQueue_Drain(t *testing.T) {
	var (
		count int
		q     = &queue.Queue{}
		inc   = func() { count += 1 }
	)
	q.AddFunc("a", inc).AddFunc("b", inc).AddFunc("c", inc)
	Equal(t, 3, len(q.Waiting()))
	Equal(t, "c", q.Drain()[2].Key)
	Equal(t, 0, len(q.Waiting()))
	Equal(t, 3, count)
}

func TestQueue_Waiting(t *testing.T) {
	var (
		q = &queue.Queue{}
		j = &queue.Job{Key: "123", Perform: func() {}}
	)
	q.Add(j)
	Equal(t, []*queue.Job{j}, q.Waiting())
	q.Drain()
	Equal(t, []*queue.Job{}, q.Waiting())
}

func TestQueue_ForcedJobQueuesAnotherJob(t *testing.T) {
	q := &queue.Queue{}
	q.AddFunc("outer", func() {
		q.AddFunc("inner", func() {})
	})
	q.Force() // Should not deadlock
}
