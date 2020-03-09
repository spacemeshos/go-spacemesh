package priorityq

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

var (
	defLen = 1000
)

func TestNewPriorityQ(t *testing.T) {
	r := require.New(t)
	pq := New(defLen)
	r.Equal(defLen, cap(pq.queues[High]))
	r.Equal(defLen, cap(pq.queues[Mid]))
	r.Equal(defLen, cap(pq.queues[Low]))
	r.Equal(prioritiesCount, len(pq.queues))
	r.Equal(prioritiesCount, cap(pq.queues))
	r.Equal(defLen*prioritiesCount, cap(pq.waitCh))
}

func TestPriorityQ_Write(t *testing.T) {
	r := require.New(t)
	pq := New(defLen)
	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(High, 0))
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(Low, 1))
		}
		wg.Done()
	}()

	wg.Wait()
	r.Equal(defLen, len(pq.queues[High]))
	r.Equal(defLen, len(pq.queues[Low]))
}

func TestPriorityQ_WriteError(t *testing.T) {
	r := require.New(t)
	pq := New(defLen)
	r.Equal(ErrUnknownPriority, pq.Write(3, 0))
}

func TestPriorityQ_Read(t *testing.T) {
	r := require.New(t)
	pq := New(defLen)
	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(High, 0))
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(Mid, 1))
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(Low, 2))
		}
		wg.Done()
	}()

	wg.Wait()

	maxPrio := 0
	rg := sync.WaitGroup{}
	rg.Add(1)
	i := 0
	go func() {
		m, e := pq.Read()
		for e == nil {
			prio, ok := m.(int)
			//fmt.Println("reading  ", m, e, i, len(pq.queues[0]), len(pq.queues[1]))
			if !ok {
				// should never happen
				t.FailNow()
			}

			r.False(prio < maxPrio)
			maxPrio = prio
			i++

			m, e = pq.Read()
		}
		rg.Done()
	}()

	time.Sleep(1500 * time.Millisecond)
	pq.Close()
	rg.Wait()
	r.Equal(3*defLen, i)
}

func TestPriorityQ_Close(t *testing.T) {
	r := require.New(t)
	pq := New(defLen)
	prios := []Priority{Low, Mid, High}
	for i := 0; i < 1000; i++ {
		r.NoError(pq.Write(Priority(i%len(prios)), []byte("LOLOLOLLZ")))
	}

	r.Equal(_getQueueSize(pq), defLen)

	c := make(chan struct{})

	go func() {
		defer func() { c <- struct{}{} }()
		time.Sleep(10 * time.Millisecond)
		i := 0
		for {
			_, err := pq.Read()
			i++
			if err != nil {
				return
			}
			if i == defLen {
				t.Error("Finished all queue while close was called midway")
			}
		}
	}()

	//close it right away
	pq.Close()
	<-c
}

func _getQueueSize(pq *Queue) int {
	i := 0
	for _, q := range pq.queues {
		i += len(q)
	}
	return i
}
