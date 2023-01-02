package priorityq

import (
	"container/heap"
	"context"
	"errors"
	"sync"
)

var (
	// ErrEmptyQueue is returned on attempt to read from an empty queue.
	ErrEmptyQueue = errors.New("attempt to read from an empty queue")
	// ErrClosed is returned when a queue is closed.
	ErrClosed = errors.New("queue is closed")
)

// PriorityQueue is the interface used to interact with the queue.
type PriorityQueue interface {
	Write(priority Priority, value any) error
	Read() (any, error)
	Length() int
}

// Priority is the type that indicates the priority of different queues.
type Priority int

const (
	// High indicates the highest priority.
	High = Priority(0)

	// Mid indicates the medium priority.
	Mid = Priority(1)

	// Low indicates the lowest priority.
	Low = Priority(2)
)

// An HeapQueueItem is something we manage in a priority queue.
type HeapQueueItem struct {
	index    int
	value    any
	priority Priority
}

// A HeapQueue implements priority.Interface using container/heap and holds Items.
type HeapQueue struct {
	ctx   context.Context
	mu    sync.Mutex
	queue []*HeapQueueItem
}

// New creates a new priority queue based on HeapQueue.
func New(ctx context.Context) PriorityQueue {
	pq := &HeapQueue{
		ctx:   ctx,
		queue: make([]*HeapQueueItem, 0),
	}
	heap.Init(pq)
	return pq
}

func (pq *HeapQueue) isClosed() bool {
	select {
	case <-pq.ctx.Done():
		return true
	default:
		return false
	}
}

// Length returns priority queue length. It's just a wrapper for Len now.
func (pq *HeapQueue) Length() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	return pq.Len()
}

// Read reads a value from the priority queue. It's a wrapper for Pop.
func (pq *HeapQueue) Read() (any, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.isClosed() {
		return nil, ErrClosed
	}

	if pq.Len() == 0 {
		return nil, ErrEmptyQueue
	}

	// We may consider adding more logic in the future, so it always returns nil error for now.
	return heap.Pop(pq).(*HeapQueueItem).value, nil
}

// Write pushes a value in the priority queue. It's a wrapper for Push.
func (pq *HeapQueue) Write(priority Priority, value any) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.isClosed() {
		return ErrClosed
	}

	heap.Push(pq, &HeapQueueItem{
		value:    value,
		priority: priority,
	})

	// We may consider adding more logic in the future, so it always returns nil error for now.
	return nil
}

// Len implements heap.Interface.
func (pq *HeapQueue) Len() int { return len(pq.queue) }

// Less implements heap.Interface.
func (pq *HeapQueue) Less(i, j int) bool {
	// Pop lowest first
	return pq.queue[i].priority < pq.queue[j].priority
}

// Swap implements heap.Interface.
func (pq *HeapQueue) Swap(i, j int) {
	pq.queue[i], pq.queue[j] = pq.queue[j], pq.queue[i]
	pq.queue[i].index = i
	pq.queue[j].index = j
}

// Push implements heap.Interface.
func (pq *HeapQueue) Push(x any) {
	n := len(pq.queue)
	item := x.(*HeapQueueItem)
	item.index = n
	pq.queue = append(pq.queue, item)
}

// Pop implements heap.Interface.
func (pq *HeapQueue) Pop() any {
	old := pq.queue
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	pq.queue = old[0 : n-1]
	return item
}
