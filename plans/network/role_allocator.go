package main

import "fmt"

type role struct {
	name      string
	count     int
	startFunc func(*role)
	doneFunc  func(obj interface{})

	storage interface{}
}

type roleAllocator struct {
	total int
	roles []*role
}

func (ra *roleAllocator) Add(name string, count int, startFunc func(*role), doneFunc func(obj interface{})) {
	ra.total += count
	ra.roles = append(ra.roles, &role{name: name, count: count, startFunc: startFunc, doneFunc: doneFunc})
}

func (ra *roleAllocator) getRole(seq int) *role {
	accumulate := 0
	for _, r := range ra.roles {
		if seq <= r.count+accumulate {
			return r
		}
		accumulate += r.count
	}
	return nil
}

func (ra *roleAllocator) Allocate(seq int) error {
	rl := ra.getRole(seq)
	if rl == nil {
		return fmt.Errorf("can't allocate %v", seq)
	}
	fmt.Println(seq, "is ", rl.name)
	rl.startFunc(rl)
	rl.doneFunc(rl.storage)
	return nil
}