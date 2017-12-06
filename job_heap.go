package queue

type jobHeap struct {
	jobs []*Job
	keys map[string]int
}

func (h *jobHeap) add(j *Job) {
	h.remove(j)
	index := 0
	for index < len(h.jobs) && h.jobs[index].runAt.Before(*j.runAt) {
		index++
	}
	h.jobs = append(h.jobs[:index], append([]*Job{j}, h.jobs[index:]...)...)
	for i := index; i < len(h.jobs); i++ {
		h.keys[h.jobs[i].Key] = i
	}
}

func (h *jobHeap) remove(j *Job) {
	if h.keys == nil {
		h.keys = make(map[string]int)
	}
	if index, ok := h.keys[j.Key]; ok {
		h.jobs = append(h.jobs[:index], h.jobs[index+1:]...)
		delete(h.keys, j.Key)
		for i := index; i < len(h.jobs); i++ {
			h.keys[h.jobs[i].Key] = i
		}
	}
}

func (h *jobHeap) has(j *Job) bool {
	_, ok := h.keys[j.Key]
	return ok
}
