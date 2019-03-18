package timesync

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type MockTimer struct {
}

func (MockTimer) Now() time.Time {
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	return start
}

func TestTicker_StartClock(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)

	ts := NewTicker(MockTimer{}, tick, start)
	tk := ts.Subscribe()
	then := time.Now()
	ts.Start()

	select {
	case <-tk:
		dur := time.Now().Sub(then)
		assert.True(t, tick < dur)
	}
	ts.Close()
}

func TestTicker_StartClock_BeforeEpoch(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:30.371Z"
	tmr := MockTimer{}
	start, _ := time.Parse(layout, str)

	waitTime := start.Sub(tmr.Now())
	ts := NewTicker(tmr, tick, start)
	tk := ts.Subscribe()
	then := time.Now()
	ts.Start()

	fmt.Println(waitTime)
	select {
	case <-tk:
		dur := time.Now().Sub(then)
		fmt.Println(dur)
		assert.True(t, waitTime < dur)
	}
	ts.Close()
}

func TestTicker_StartClock_LayerID(t *testing.T) {
	tick := 1 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:20.371Z"
	start, _ := time.Parse(layout, str)

	ts := NewTicker(MockTimer{}, tick, start)
	ts.updateLayerID()
	assert.Equal(t, 6, int(ts.currentLayer))
	ts.Close()
}

type MyTimer struct {
}

func (mt *MyTimer) Now() time.Time {
	return time.Now()
}

func TestTicker_StartClock_2(t *testing.T) {
	destTime := 2 * time.Second
	tmr := &MyTimer{}
	then := tmr.Now()
	ticker := NewTicker(tmr, 5*time.Second, then.Add(destTime))
	ticker.Start()
	sub := ticker.Subscribe()
	<-sub
	assert.True(t, tmr.Now().Sub(then).Seconds() <= float64(2.1))
}
