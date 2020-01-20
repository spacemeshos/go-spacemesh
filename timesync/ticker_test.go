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

type mockClock struct {
	now time.Time
}

func newMockClock() *mockClock {
	return &mockClock{now: getTime()}
}

func (m *mockClock) advance(toAdd time.Duration) {
	m.now = m.now.Add(toAdd)
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func TestNewTicker(t *testing.T) {
	r := require.New(t)
	tr := NewTicker(newMockClock(), conv)
	r.NotNil(tr.subs)
	r.False(tr.started)
}

func TestTicker_StartNotifying(t *testing.T) {
	r := require.New(t)
	cl := newMockClock()
	mc := newMockConv(1, cl.Now())
	tr := NewTicker(cl, mc)
	_, e := tr.Notify()
	r.Equal(1, mc.countToLayer)
	r.Equal(errNotStarted, e)
	tr.StartNotifying()
	r.True(tr.started)
	_, e = tr.Notify()
	r.NoError(e)
	r.Equal(3, mc.countToLayer)
}

func TestTicker_Notify(t *testing.T) {
	r := require.New(t)
	cl := newMockClock()
	tr := NewTicker(cl, newMockConv(1, cl.Now()))
	tr.StartNotifying()
	tr.lastTickedLayer = 3
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
	c := newMockClock()
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
	c := newMockClock()
	tr := NewTicker(c, LayerConv{
		duration: 100 * time.Millisecond,
		genesis:  c.Now().Add(-320 * time.Millisecond),
	})
	r.True(tr.timeSinceLastTick() >= 20)
}

func TestTicker_AwaitLayer(t *testing.T) {
	r := require.New(t)

	tmr := newMockClock()
	ticker := NewTicker(tmr, LayerConv{
		duration: 10 * time.Millisecond,
		genesis:  tmr.Now(),
	})

	l := ticker.GetCurrentLayer() + 1
	ch := ticker.AwaitLayer(l)

	select {
	case <-ch:
		r.Fail("got notified before layer ticked")
	default:
	}

	tmr.advance(10 * time.Millisecond)
	ticker.StartNotifying()
	missedTicks, err := ticker.Notify()
	r.NoError(err)
	r.Zero(missedTicks)

	select {
	case <-ch:
	default:
		r.Fail("did not get notified despite layer ticking")
	}

	ch2 := ticker.AwaitLayer(l)

	r.NotEqual(ch, ch2) // original channel should be discarded and a constant closedChannel should be returned
	select {
	case <-ch2:
	default:
		r.Fail("returned channel was not closed, despite awaiting past layer")
	}

	ch3 := ticker.AwaitLayer(l - 1)

	r.Equal(ch2, ch3) // the same closedChannel should be returned for all past layers
}

func TestTicker_AwaitLayerOldSubs(t *testing.T) {
	// check that after skipping layers the old layers are released
	r := require.New(t)
	c := &mockClock{now: time.Now()}
	lDur := 50 * time.Millisecond
	tr := NewTicker(c, LayerConv{
		duration: lDur,
		genesis:  c.Now(),
	})
	tr.StartNotifying()
	ch := tr.AwaitLayer(5) // sub to layer 5
	c.advance(lDur * 2)    // clock advanced only two layers
	tr.Notify()            // notify called (before layer 5)
	select {
	case <-ch:
		r.FailNow(t.Name() + "released before layer 5")
	default:
	}
	c.advance(lDur * 10) // now we passed layer 5 by a few layers
	tr.Notify()          // this should release ch since layer 5 is in the past

	select {
	case <-ch:
	default:
		r.FailNow(t.Name() + "timed out")
	}
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
