package queue_test

import (
	"github.com/hx/queue"
	"reflect"
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
		AddFunc(func() { run = true }).
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
		AddFunc(func() { panic("abc") }).
		WaitAll()
	Equal(t, "abc", caught)
}

func TestQueue_Exclusive(t *testing.T) {
	seq := ""
	job := &queue.Job{
		Key: "stuff",
		Perform: func() {
			seq += "a"
			time.Sleep(20 * time.Millisecond)
			seq += "z"
		},
	}
	q := queue.NewQueue().Work(3)
	q.Add(job)
	time.Sleep(10 * time.Millisecond)
	q.Add(job).WaitAll()
	job.Simultaneous = true
	q.Add(job)
	time.Sleep(10 * time.Millisecond)
	q.Add(job).WaitAll()
	Equal(t, "azazaazz", seq)
}