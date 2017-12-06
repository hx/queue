package queue

import "sync"

type jobHeap struct {
	jobs []*Job
	keys map[string]int
	wg   sync.WaitGroup
}

func (h *jobHeap) add(j *Job) {
	h.wg.Add(1)
	h.remove(j.Key)
	index := 0
	for index < len(h.jobs) && h.jobs[index].runAt.Before(*j.runAt) {
		index++
	}
	h.jobs = append(h.jobs[:index], append([]*Job{j}, h.jobs[index:]...)...)
	for i := index; i < len(h.jobs); i++ {
		h.keys[h.jobs[i].Key] = i
	}
}

func (h *jobHeap) remove(key string) bool {
	if h.keys == nil {
		h.keys = make(map[string]int)
	}
	if index, ok := h.keys[key]; ok {
		h.jobs = append(h.jobs[:index], h.jobs[index+1:]...)
		delete(h.keys, key)
		for i := index; i < len(h.jobs); i++ {
			h.keys[h.jobs[i].Key] = i
		}
		h.wg.Done()
		return true
	}
	return false
}

func (h *jobHeap) wait() {
	h.wg.Wait()
}
