package gossip

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

var (
	name1 = "name1"
	name2 = "name2"
	name3 = "name3"

	defLen = 1000
)

func TestPriorityQ_Set(t *testing.T) {
	r := require.New(t)
	pq := NewPriorityQ(10)
	r.Equal(10, len(pq.queues))

	// add prio 0
	r.NoError(pq.Set(name1, 0, 10))
	r.Equal(1, len(pq.prios))
	r.NotNil(pq.queues[0])

	// add again for same name
	r.Equal(ErrAlreadySet, pq.Set(name1, 2, 10))

	// add prio 0 again for different name
	r.NoError(pq.Set(name2, 0, 10))
	r.Equal(2, len(pq.prios))
}

func TestPriorityQ_Write(t *testing.T) {
	r := require.New(t)
	pq := NewPriorityQ(10)
	r.NoError(pq.Set(name1, 0, defLen))
	r.NoError(pq.Set(name2, 1, defLen))
	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(name1, 0))
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(name2, 1))
		}
		wg.Done()
	}()

	wg.Wait()
	r.Equal(defLen, len(pq.queues[0]))
	r.Equal(defLen, len(pq.queues[1]))
}

func TestPriorityQ_Read(t *testing.T) {
	r := require.New(t)
	pq := NewPriorityQ(10)
	r.NoError(pq.Set(name1, 0, defLen))
	r.NoError(pq.Set(name2, 1, defLen))
	r.NoError(pq.Set(name3, 2, defLen))
	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(name1, int(0)))
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(name2, int(1)))
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		for i := 0; i < defLen; i++ {
			r.NoError(pq.Write(name3, int(2)))
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
			//fmt.Println("reading  ", m, e, i, len(pq.queues[0]), len(pq.queues[1]))
			i++
			prio, ok := m.(int)
			if !ok {
				break
			}
			r.False(prio < maxPrio)
			maxPrio = prio

			m, e = pq.Read()
		}
		rg.Done()
	}()

	time.Sleep(1500 * time.Millisecond)
	pq.Close()
	rg.Wait()
	r.Equal(3*defLen, i-1)
}
