package priorityq

import (
	"errors"
)

var (
	ErrUnknownPriority = errors.New("unknown priority")
	ErrQueueClosed     = errors.New("the queue is closed")
)

type Priority int

const (
	prioritiesCount = 3 // the number of priorities
	High            = Priority(0)
	Mid             = Priority(1)
	Low             = Priority(2)
)

type PriorityQ struct {
	waitCh chan struct{}
	queues []chan interface{}
}

// NewPriorityQ returns a new priority queue where each priority has a buffer of prioQueueLimit
func NewPriorityQ(prioQueueLimit int) *PriorityQ {
	// set queue for each priority
	qs := make([]chan interface{}, prioritiesCount)
	for i := range qs {
		qs[i] = make(chan interface{}, prioQueueLimit)
	}

	return &PriorityQ{
		waitCh: make(chan struct{}, prioQueueLimit*prioritiesCount),
		queues: qs,
	}
}

// Write a message m to the associated queue with the provided priority
// This method blocks iff the queue is full
// Returns an error iff a queue does not exist for the provided name
// Note: writing to the pq after a call to close is forbidden and will result in a panic
func (pq *PriorityQ) Write(prio Priority, m interface{}) error {
	if int(prio) >= cap(pq.queues) {
		return ErrUnknownPriority
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
		if q == nil { // if not set just continue
			continue
		}

		// if set, try read
		select {
		case m := <-q:
			return m, nil
		default: // empty, try next
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
		if q != nil {
			close(q)
		}
	}
	close(pq.waitCh)
}
