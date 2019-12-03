package gossip

import "errors"

var (
	ErrAlreadySet = errors.New("a queue is already associated with this name")
	ErrNotExist   = errors.New("no queue associated with this name")
)

type prioNode struct {
	c    chan interface{}
	prio int
	next *prioNode
}

type PriorityQ struct {
	waitCh chan struct{}
	queues map[string]*prioNode
	size   int
	head   *prioNode // the list of queues
}

func NewPriorityQ() *PriorityQ {
	return &PriorityQ{
		waitCh: make(chan struct{}),
		queues: make(map[string]*prioNode),
		head:   nil,
	}
}

// Set the name of the queue with to the provided priority and buffer size
// Returns an error if the name is already associated with a queue
// Note: all calls to Set are assumed to be called before any call to Read/Write
func (pq *PriorityQ) Set(name string, prio int, bufferSize int) error {
	if _, exist := pq.queues[name]; exist {
		return ErrAlreadySet
	}

	pq.waitCh = make(chan struct{}, cap(pq.waitCh)+bufferSize) // update max concurrent writes
	c := make(chan interface{}, bufferSize)

	if pq.head == nil {
		pn := &prioNode{c, prio, nil}
		pq.head = pn
		pq.queues[name] = pn
		return nil
	}

	if prio <= pq.head.prio {
		newHead := &prioNode{, prio, pq.head}
		pq.head = newHead
		pq.queues[name] = newHead
	}

	curr := pq.head
	for curr.next != nil && curr.next.prio > prio { // find position
		curr = curr.next
	}
	pn := &prioNode{c, prio, curr.next}
	curr.next = pn
	pq.queues[name] = pn

	return nil
}

func (pq *PriorityQ) Write(name string, m interface{}) error {
	q, exist := pq.queues[name]
	if !exist {
		return ErrNotExist
	}

	q.c <- m
	pq.waitCh <- struct{}{}
	return nil
}

func (pq *PriorityQ) Read() interface{} {
	<-pq.waitCh // wait for m

	// pick by priority
	curr := pq.head
	for {
		select {
		case m := <-curr.c:
			return m
		default:
			curr = curr.next
		}
	}

	// should be unreachable
	return nil
}
