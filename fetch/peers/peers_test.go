package peers

import (
	"encoding/binary"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"
)

// any random non zero number that will be used if size is not specified in the test case
// it is intentionally different from assumed minimal size in the latency function.
const testSize = 100

type event struct {
	id          peer.ID
	add, delete bool
	size        int
	success     int
	failure     int
	latency     time.Duration
	protocols   []protocol.ID
}

func withEvents(events []event) *Peers {
	tracker := New()
	for _, ev := range events {
		if ev.delete {
			tracker.Delete(ev.id)
		} else if ev.add {
			tracker.Add(ev.id, func() []protocol.ID { return ev.protocols })
		}
		for i := 0; i < ev.failure; i++ {
			tracker.OnFailure(ev.id, 0, ev.latency)
		}
		for i := 0; i < ev.success; i++ {
			tracker.OnLatency(ev.id, max(ev.size, testSize), ev.latency)
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

		selectFrom []peer.ID
		best       peer.ID
	}{
		{
			desc: "latency adjusted with more requests",
			events: []event{
				{id: "a", success: 1, latency: 8, add: true},
				{id: "b", success: 1, latency: 9, add: true},
				{id: "a", success: 3, latency: 14, add: true},
			},
			n:          5,
			expect:     []peer.ID{"b", "a"},
			selectFrom: []peer.ID{"a", "b"},
			best:       peer.ID("b"),
		},
		{
			desc: "latency computed with moving average",
			events: []event{
				{id: "a", success: 2, latency: 8, add: true},
				{id: "b", success: 2, latency: 9, add: true},
			},
			n:          5,
			expect:     []peer.ID{"a", "b"},
			selectFrom: []peer.ID{"b", "a"},
			best:       peer.ID("a"),
		},
		{
			desc: "change average on failure",
			events: []event{
				{id: "a", failure: 2, latency: 8, add: true},
				{id: "b", failure: 2, latency: 9, add: true},
			},
			n:          5,
			expect:     []peer.ID{"a", "b"},
			selectFrom: []peer.ID{"b", "a"},
			best:       peer.ID("a"),
		},
		{
			desc: "same latency different fail rate",
			events: []event{
				{id: "a", success: 2, failure: 2, latency: 8, add: true},
				{id: "b", success: 4, failure: 2, latency: 9, add: true},
			},
			n:          5,
			expect:     []peer.ID{"b", "a"},
			selectFrom: []peer.ID{"b", "a"},
			best:       peer.ID("b"),
		},
		{
			desc: "latency adjusted based on size",
			events: []event{
				{id: "a", success: 2, latency: 10, size: 1_000, add: true},
				{id: "b", success: 2, latency: 20, size: 4_000, add: true},
			},
			n:          5,
			expect:     []peer.ID{"b", "a"},
			selectFrom: []peer.ID{"a", "b"},
			best:       peer.ID("b"),
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
			n:          4,
			expect:     []peer.ID{"c", "d"},
			selectFrom: []peer.ID{"a", "b", "c", "d"},
			best:       peer.ID("c"),
		},
		{
			desc:       "empty",
			n:          4,
			selectFrom: []peer.ID{"a", "b", "c", "d"},
		},
		{
			desc: "request empty",
			events: []event{
				{id: "a", success: 100, add: true},
			},
			n: 0,
		},
		{
			desc: "events for nonexisting",
			events: []event{
				{id: "a", success: 100, failure: 100},
			},
			n: 2,
		},
		{
			desc: "new peer",
			events: []event{
				{id: "a", success: 1, latency: 10, add: true},
				{id: "b", add: true},
			},
			n:          2,
			expect:     []peer.ID{"b", "a"},
			selectFrom: []peer.ID{"a", "b"},
			best:       peer.ID("b"),
		},
		{
			desc: "unresponsive",
			events: []event{
				{id: "a", success: 1, latency: 10, add: true},
				{id: "b", failure: 1, latency: 10, add: true},
			},
			n:          2,
			expect:     []peer.ID{"a", "b"},
			selectFrom: []peer.ID{"a", "b"},
			best:       peer.ID("a"),
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
			if tc.selectFrom != nil {
				require.Equal(
					t,
					tc.best,
					withEvents(tc.events).SelectBestFrom(tc.selectFrom),
					"select best (%v) from %v",
					tc.best,
					tc.selectFrom,
				)
			}
		})
	}
}

func TestSelectBestWithProtocols(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		events []event

		n         int
		protocols []protocol.ID
		expect    []peer.ID
	}{
		{
			desc: "no protocols required and no peer protocols",
			events: []event{
				{id: "a", success: 1, latency: 8, add: true},
				{id: "b", success: 1, latency: 9, add: true},
				{id: "c", success: 3, latency: 14, add: true},
			},
			n:         2,
			expect:    []peer.ID{"a", "b"},
			protocols: nil,
		},
		{
			desc: "no protocols required, peers have protocols",
			events: []event{
				{id: "a", success: 1, latency: 8, add: true, protocols: []protocol.ID{"a", "b"}},
				{id: "b", success: 1, latency: 9, add: true, protocols: []protocol.ID{"b", "c"}},
				{id: "c", success: 3, latency: 14, add: true, protocols: []protocol.ID{"c", "d"}},
			},
			n:         2,
			expect:    []peer.ID{"a", "b"},
			protocols: nil,
		},
		{
			desc: "single protocol required, peers have protocols",
			events: []event{
				{id: "a", success: 1, latency: 8, add: true, protocols: []protocol.ID{"a", "b"}},
				{id: "b", success: 1, latency: 9, add: true, protocols: []protocol.ID{"b", "c"}},
				{id: "c", success: 3, latency: 14, add: true, protocols: []protocol.ID{"c", "d"}},
			},
			n:         2,
			expect:    []peer.ID{"b", "c"},
			protocols: []protocol.ID{"c"},
		},
		{
			desc: "multiple protocols required, peers have protocols",
			events: []event{
				{id: "a", success: 1, latency: 8, add: true, protocols: []protocol.ID{"a", "b"}},
				{id: "b", success: 1, latency: 9, add: true, protocols: []protocol.ID{"b", "c"}},
				{id: "c", success: 3, latency: 14, add: true, protocols: []protocol.ID{"c", "d"}},
				{id: "d", success: 3, latency: 12, add: true, protocols: []protocol.ID{"a", "e"}},
			},
			n:         3,
			expect:    []peer.ID{"a", "b", "c"},
			protocols: []protocol.ID{"b", "c"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(
				t,
				tc.expect,
				withEvents(tc.events).SelectBestWithProtocols(tc.n, tc.protocols),
				"select best %d",
				tc.n,
			)
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
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:], 10001)
	rng := rand.New(rand.NewChaCha8(seed))

	for i := 0; i < total; i++ {
		events = append(
			events,
			event{
				id:      peer.ID(strconv.Itoa(i)),
				success: rng.IntN(100),
				failure: rng.IntN(100),
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
