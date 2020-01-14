package timesync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

var conv = LayerConv{
	duration: 1 * time.Second,
	genesis:  getTime(),
}

type mockConv struct {
	next         types.LayerID
	countToLayer int
	t            time.Time
	countToTime  int
}

func newMockConv(l types.LayerID, t time.Time) *mockConv {
	return &mockConv{
		next: l,
		t:    t,
	}
}

func (m *mockConv) TimeToLayer(time.Time) types.LayerID {
	defer func() {
		m.next++
		m.countToLayer++
	}()
	return m.next
}

func (m *mockConv) LayerToTime(types.LayerID) time.Time {
	m.countToTime++
	return m.t
}

func TestNewTicker(t *testing.T) {
	r := require.New(t)
	tr := NewTicker(RealClock{}, conv)
	r.NotNil(tr.subs)
	r.False(tr.started)
}

func TestTicker_StartNotifying(t *testing.T) {
	r := require.New(t)
	mc := newMockConv(1, time.Now())
	tr := NewTicker(RealClock{}, mc)
	_, e := tr.Notify()
	r.Equal(1, mc.countToLayer)
	r.Equal(errNotStarted, e)
	tr.StartNotifying()
	r.True(tr.started)
	_, e = tr.Notify()
	r.NoError(e)
	r.Equal(2, mc.countToLayer)
}

func TestTicker_Notify(t *testing.T) {
	r := require.New(t)
	tr := NewTicker(RealClock{}, newMockConv(1, time.Now()))
	tr.StartNotifying()
	tr.lastTickedLayer = 2
	_, e := tr.Notify() // should fail to send
	r.Equal(errNotMonotonic, e)

	tr.lastTickedLayer = 0
	s1 := tr.Subscribe()
	s2 := tr.Subscribe()
	wg := sync.WaitGroup{} // higher probability that s2 is listening state before notify
	wg.Add(1)
	go func() {
		wg.Done()
		<-s2 // read only from second
	}()
	wg.Wait()
	missed, e := tr.Notify() // should send only to one

	tr.m.Lock() // make sure done notify
	tr.m.Unlock()
	r.Equal(errMissedTicks, e)
	r.Equal(1, missed)

	wg = sync.WaitGroup{}
	wg.Add(1)
	go func() {
		wg.Done()
		<-s1
	}()
	wg.Add(1)
	go func() {
		wg.Done()
		<-s2
	}()
	wg.Wait()
	missed, e = tr.Notify() // should send to all
	tr.m.Lock()             // make sure done notify
	tr.m.Unlock()
	r.Equal(0, missed)
	r.NoError(e)
}

func TestTicker_NotifyErrThreshold(t *testing.T) {
	r := require.New(t)
	c := RealClock{}
	ld := 10000 * time.Millisecond
	tr := NewTicker(c, LayerConv{
		duration: ld,
		genesis:  c.Now().Add(-ld * 3).Add(sendTickThreshold),
	})
	tr.StartNotifying()
	_, e := tr.Notify() // notify is called after more than the threshold
	r.Equal(errMissedTickTime, e)
}

func TestTicker_timeSinceLastTick(t *testing.T) {
	r := require.New(t)
	tr := NewTicker(RealClock{}, LayerConv{
		duration: 100 * time.Millisecond,
		genesis:  time.Now().Add(-320 * time.Millisecond),
	})
	r.True(tr.timeSinceLastTick() >= 20)
}

func TestSubs_SubscribeUnsubscribe(t *testing.T) {
	r := require.New(t)
	subs := newSubs()
	r.Equal(0, len(subs.subscribers))
	c1 := subs.Subscribe()
	r.Equal(1, len(subs.subscribers))

	c2 := subs.Subscribe()
	r.Equal(2, len(subs.subscribers))

	subs.Unsubscribe(c1)
	r.Equal(1, len(subs.subscribers))
	_, ok := subs.subscribers[c1]
	r.False(ok)

	subs.Unsubscribe(c2)
	r.Equal(0, len(subs.subscribers))
}
