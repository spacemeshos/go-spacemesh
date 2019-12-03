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
func (pq *PriorityQ) Set(name string, prio int, bufferSize int) error {
	if _, exist := pq.prios[name]; exist {
		return ErrAlreadySet
	}

	pq.waitCh = make(chan struct{}, cap(pq.waitCh)+bufferSize) // update max concurrent writes
	pq.prios[name] = prio
	pq.queues[prio] = make(chan interface{}, bufferSize)

	return nil
}

func (pq *PriorityQ) Write(name string, m interface{}) error {
	prio, exist := pq.prios[name]
	if !exist {
		return ErrNotExist
	}

	pq.queues[prio] <- m
	pq.waitCh <- struct{}{}
	return nil
}

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

func (pq *PriorityQ) Close() {
	for _, q := range pq.queues {
		close(q)
	}
	close(pq.waitCh)
}
