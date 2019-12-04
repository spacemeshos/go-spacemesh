package gossip

import "errors"

var (
	ErrAlreadySet  = errors.New("a queue is already associated with this name")
	ErrNotExist    = errors.New("no queue associated with this name")
	ErrQueueClosed = errors.New("the queue is closed")
)

type PriorityQ struct {
	waitCh chan struct{}
	prios  map[string]int
	queues []chan interface{}
}

func NewPriorityQ(queueCount int) *PriorityQ {
	return &PriorityQ{
		waitCh: make(chan struct{}),
		prios:  make(map[string]int, queueCount),
		queues: make([]chan interface{}, queueCount),
	}
}

// Set the name of the queue with to the provided priority and buffer size
// Returns an error if the name is already associated with a queue
// Note: all calls to Set are assumed to be called before any call to Read/Write
// Note: multiple names may be associated with the same priority
func (pq *PriorityQ) Set(name string, prio int, bufferSize int) error {
	if _, exist := pq.prios[name]; exist {
		return ErrAlreadySet
	}

	pq.prios[name] = prio
	if pq.queues[prio] != nil { // allow multiple names to be attached to the same priority
		pq.queues[prio] = make(chan interface{}, bufferSize) // create queue for prio
		pq.waitCh = make(chan struct{}, cap(pq.waitCh)+bufferSize) // update max concurrent writes
	}

	return nil
}

// Write a message m to the associated queue with the provided name
// Blocks iff the queue is full
// Returns an error iff a queue does not exist for the provided name
// Note: writing to the pq after a call to close will panic
func (pq *PriorityQ) Write(name string, m interface{}) error {
	prio, exist := pq.prios[name]
	if !exist {
		return ErrNotExist
	}

	pq.queues[prio] <- m
	pq.waitCh <- struct{}{}
	return nil
}

// Read returns the next message by priority
// An error is set iff the priority queue has been closed
func (pq *PriorityQ) Read() (interface{}, error) {
	<-pq.waitCh // wait for m

	// pick by priority
	for _, q := range pq.queues {
		select {
		case m := <-q:
			return m, nil
		default:
			continue
		}
	}

	// should be unreachable
	return nil, ErrQueueClosed
}

// Close the priority queue
// No messages should be expected to be read after a call to Close
func (pq *PriorityQ) Close() {
	for _, q := range pq.queues {
		close(q)
	}
	close(pq.waitCh)
}
