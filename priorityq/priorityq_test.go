package priorityq

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var defLen = 1000

func TestPriorityQ_Write(t *testing.T) {
	r := require.New(t)
	pq := New(context.Background())
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
	r.Equal(defLen*2, pq.Length())
}

func TestPriorityQ_Read(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pq := New(ctx)
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
			// t.Logln("reading  ", m, e, i, len(pq.queues[0]), len(pq.queues[1]))
			if !ok {
				// should never happen
				require.FailNow(t, "unable to read message priority")
			}

			r.False(prio < maxPrio)
			maxPrio = prio
			i++

			m, e = pq.Read()
		}
		rg.Done()
	}()

	time.Sleep(1500 * time.Millisecond)
	cancel()
	rg.Wait()
	r.Equal(3*defLen, i)
}

func TestPriorityQ_Close(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pq := New(ctx)
	prios := []Priority{Low, Mid, High}
	for i := 0; i < 1000; i++ {
		r.NoError(pq.Write(Priority(i%len(prios)), []byte("LOLOLOLLZ")))
	}

	r.Equal(pq.Length(), defLen)

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

	// close it right away
	cancel()
	<-c
}
