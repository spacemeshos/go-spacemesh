package peers

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

type event struct {
	id          peer.ID
	add, delete bool
	success     int
	failure     int
	latency     time.Duration
}

func withEvents(events []event) *Peers {
	tracker := New(WithRateThreshold(0.1))
	for _, ev := range events {
		if ev.delete {
			tracker.Delete(ev.id)
		} else if ev.add {
			tracker.Add(ev.id)
		}
		for i := 0; i < ev.failure; i++ {
			tracker.OnFailure(ev.id)
		}
		for i := 0; i < ev.success; i++ {
			tracker.OnLatency(ev.id, ev.latency)
		}
	}
	return tracker
}

func TestSelect(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		events []event

		n      int
		expect []peer.ID

		from []peer.ID
		best peer.ID
	}{
		{
			desc: "ordered by rate",
			events: []event{
				{id: "b", success: 100, failure: 30, add: true},
				{id: "c", success: 100, failure: 0, add: true},
				{id: "a", success: 80, failure: 80, add: true},
			},
			n:      5,
			expect: []peer.ID{"c", "b", "a"},
			from:   []peer.ID{"a", "b"},
			best:   peer.ID("b"),
		},
		{
			desc: "ordered by rate no best",
			events: []event{
				{id: "b", success: 100, failure: 30, add: true},
				{id: "c", success: 100, failure: 0, add: true},
				{id: "a", success: 80, failure: 80, add: true},
			},
			n:      5,
			expect: []peer.ID{"c", "b", "a"},
			from:   []peer.ID{"d", "e"},
			best:   "",
		},
		{
			desc: "ordered by latency within threshold",
			events: []event{
				{id: "b", success: 100, latency: 10, add: true},
				{id: "c", success: 95, latency: 5, add: true},
				{id: "a", success: 90, latency: 4, add: true},
			},
			n:      5,
			expect: []peer.ID{"a", "c", "b"},
			from:   []peer.ID{"c", "a"},
			best:   peer.ID("a"),
		},
		{
			desc: "latency computed with moving average",
			events: []event{
				{id: "a", success: 100, latency: 8, add: true},
				{id: "b", success: 100, latency: 9, add: true},
				{id: "a", success: 1, latency: 10, add: true},
			},
			n:      5,
			expect: []peer.ID{"a", "b"},
			from:   []peer.ID{"a", "b"},
			best:   peer.ID("a"),
		},
		{
			desc: "latency computed with moving average",
			events: []event{
				{id: "a", success: 100, latency: 8, add: true},
				{id: "b", success: 100, latency: 9, add: true},
				{id: "a", success: 1, latency: 14},
			},
			n:      5,
			expect: []peer.ID{"b", "a"},
			from:   []peer.ID{"a", "b"},
			best:   peer.ID("b"),
		},
		{
			desc: "total number is larger then capacity",
			events: []event{
				{id: "a", success: 100, add: true},
				{id: "b", success: 80, failure: 20, add: true},
				{id: "c", success: 60, failure: 40, add: true},
				{id: "d", success: 40, failure: 60, add: true},
			},
			n:      2,
			expect: []peer.ID{"a", "b"},
		},
		{
			desc: "total number is larger then capacity",
			events: []event{
				{id: "a", success: 100, add: true},
				{id: "b", success: 80, failure: 20, add: true},
				{id: "c", success: 60, failure: 40, add: true},
				{id: "d", success: 40, failure: 60, add: true},
			},
			n:      2,
			expect: []peer.ID{"a", "b"},
		},
		{
			desc: "deleted are not in the list",
			events: []event{
				{id: "a", success: 100, add: true},
				{id: "b", success: 80, failure: 20, add: true},
				{id: "c", success: 60, failure: 40, add: true},
				{id: "d", success: 40, failure: 60, add: true},
				{id: "b", delete: true},
				{id: "a", delete: true},
			},
			n:      4,
			expect: []peer.ID{"c", "d"},
			from:   []peer.ID{"a", "b", "c", "d"},
			best:   peer.ID("c"),
		},
		{
			desc: "empty",
			n:    4,
		},
		{
			desc: "request empty",
			events: []event{
				{id: "a", success: 100, add: true},
			},
			n: 0,
		},
		{
			desc: "no success rate",
			events: []event{
				{id: "a", add: true},
				{id: "b", add: true},
			},
			n:      2,
			expect: []peer.ID{"b", "a"},
		},
		{
			desc: "events for nonexisting",
			events: []event{
				{id: "a", success: 100, failure: 100},
			},
			n: 2,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(
				t,
				tc.expect,
				withEvents(tc.events).SelectBest(tc.n),
				"select best %d",
				tc.n,
			)
			if tc.from != nil {
				require.Equal(
					t,
					tc.best,
					withEvents(tc.events).SelectBestFrom(tc.from),
					"select best (%v) from %v",
					tc.best,
					tc.from,
				)
			}
		})
	}
}

func TestTotal(t *testing.T) {
	const total = 100
	events := []event{}
	for i := 0; i < total; i++ {
		events = append(
			events, event{id: peer.ID(strconv.Itoa(i)), add: true},
		)
	}
	require.Equal(t, total, withEvents(events).Total())
}

func BenchmarkSelectBest(b *testing.B) {
	const (
		total  = 10000
		target = 10
	)
	events := []event{}
	rng := rand.New(rand.NewSource(10001))

	for i := 0; i < total; i++ {
		events = append(
			events,
			event{
				id:      peer.ID(strconv.Itoa(i)),
				success: rng.Intn(100),
				failure: rng.Intn(100),
				add:     true,
			},
		)
	}
	tracker := withEvents(events)
	require.Equal(b, total, tracker.Total())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		best := tracker.SelectBest(target)
		if len(best) != target {
			b.Fail()
		}
	}
}
