package rangesync_test

import (
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// fakeConduit is a fake Conduit for testing purposes that connects two
// RangeSetReconcilers together without any network connection.
type fakeConduit struct {
	tb   testing.TB
	msgs []rangesync.SyncMessage
	resp []rangesync.SyncMessage
	rec  []rangesync.SyncMessage
}

var _ rangesync.Conduit = &fakeConduit{}

func (fc *fakeConduit) gotoResponse() {
	fc.msgs = fc.resp
	fc.resp = nil
}

func (fc *fakeConduit) numItems() int {
	n := 0
	for _, m := range fc.msgs {
		n += len(m.Keys())
	}
	return n
}

func (fc *fakeConduit) NextMessage() (rangesync.SyncMessage, error) {
	if len(fc.msgs) != 0 {
		m := fc.msgs[0]
		fc.msgs = fc.msgs[1:]
		return m, nil
	}

	return nil, nil
}

func (fc *fakeConduit) Send(msg rangesync.SyncMessage) error {
	fc.resp = append(fc.resp, msg)
	fc.rec = append(fc.rec, msg)
	return nil
}

func makeSet(items string) *rangesync.DumbSet {
	s := &rangesync.DumbSet{
		FPFunc: rangesync.NaiveFPFunc,
	}
	for _, c := range []byte(items) {
		s.AddUnchecked(rangesync.KeyBytes{c})
	}
	return s
}

func setStr(os rangesync.OrderedSet) string {
	ids, err := os.Items().Collect()
	if err != nil {
		panic("set error: " + err.Error())
	}
	var r strings.Builder
	for _, id := range ids {
		r.Write(id[:1])
	}
	return r.String()
}

// NOTE: when enabled, this produces A LOT of output during tests (116k+ lines), which
// may be too much if you run the tests in the verbose mode.
// But it's useful for debugging and understanding how sync works, so it's left here for
// now.
var showMessages = false

func dumpRangeMessages(tb testing.TB, msgs []rangesync.SyncMessage, fmt string, args ...any) {
	if !showMessages {
		return
	}
	tb.Logf(fmt, args...)
	for _, m := range msgs {
		tb.Logf("  %s", rangesync.SyncMessageToString(m))
	}
}

func runSync(
	tb testing.TB,
	syncA, syncB *rangesync.RangeSetReconciler,
	x, y rangesync.KeyBytes,
	maxRounds int,
) (nRounds, nMsg, nItems int) {
	fc := &fakeConduit{tb: tb}
	require.NoError(tb, syncA.Initiate(fc, x, y))
	return doRunSync(fc, syncA, syncB, maxRounds)
}

func doRunSync(fc *fakeConduit, syncA, syncB *rangesync.RangeSetReconciler, maxRounds int) (nRounds, nMsg, nItems int) {
	var i int
	aDone, bDone := false, false
	dumpRangeMessages(fc.tb, fc.resp, "A %q -> B %q (init):",
		setStr(syncA.Set()),
		setStr(syncB.Set()))
	dumpRangeMessages(fc.tb, fc.resp, "A -> B (init):")
	for i = 0; ; i++ {
		if i == maxRounds {
			require.FailNow(fc.tb, "too many rounds", "didn't reconcile in %d rounds", i)
		}
		fc.gotoResponse()
		nMsg += len(fc.msgs)
		nItems += fc.numItems()
		var err error
		bDone, err = syncB.DoRound(rangesync.Sender{fc})
		require.NoError(fc.tb, err)
		// a party should never send anything in response to the "done" message
		require.False(fc.tb, aDone && !bDone, "A is done but B after that is not")
		dumpRangeMessages(fc.tb, fc.resp, "B %q -> A %q:",
			setStr(syncA.Set()),
			setStr(syncB.Set()))
		dumpRangeMessages(fc.tb, fc.resp, "B -> A:")
		if aDone && bDone {
			require.Empty(fc.tb, fc.resp, "got messages from B in response to done msg from A")
			break
		}
		fc.gotoResponse()
		nMsg += len(fc.msgs)
		nItems += fc.numItems()
		aDone, err = syncA.DoRound(rangesync.Sender{fc})
		require.NoError(fc.tb, err)
		dumpRangeMessages(fc.tb, fc.msgs, "A %q --> B %q:",
			setStr(syncA.Set()),
			setStr(syncB.Set()))
		dumpRangeMessages(fc.tb, fc.resp, "A -> B:")
		require.False(fc.tb, bDone && !aDone, "B is done but A after that is not")
		if aDone && bDone {
			require.Empty(fc.tb, fc.resp, "got messages from A in response to done msg from B")
			break
		}
	}
	return i + 1, nMsg, nItems
}

func runProbe(tb testing.TB, from, to *rangesync.RangeSetReconciler, x, y rangesync.KeyBytes) rangesync.ProbeResult {
	fc := &fakeConduit{tb: tb}
	info, err := from.InitiateProbe(fc, x, y)
	require.NoError(tb, err)
	return doRunProbe(fc, from, to, info)
}

func doRunProbe(
	fc *fakeConduit,
	from, to *rangesync.RangeSetReconciler,
	info rangesync.RangeInfo,
) rangesync.ProbeResult {
	require.NotEmpty(fc.tb, fc.resp, "empty initial round")
	fc.gotoResponse()
	done, err := to.DoRound(rangesync.Sender{fc})
	require.True(fc.tb, done)
	require.NoError(fc.tb, err)
	fc.gotoResponse()
	pr, err := from.HandleProbeResponse(fc, info)
	require.NoError(fc.tb, err)
	require.Nil(fc.tb, fc.resp, "got messages from Probe in response to done msg")
	return pr
}

func TestRangeSync(t *testing.T) {
	for _, tc := range []struct {
		name           string
		a, b           string
		finalA, finalB string
		x, y           string
		countA, countB int
		inSync         bool
		maxRounds      [4]int
		sim            float64
	}{
		{
			name:      "empty sets",
			a:         "",
			b:         "",
			finalA:    "",
			finalB:    "",
			countA:    0,
			countB:    0,
			inSync:    true,
			maxRounds: [4]int{1, 1, 1, 1},
			sim:       1,
		},
		{
			name:      "empty to non-empty",
			a:         "",
			b:         "abcd",
			finalA:    "abcd",
			finalB:    "abcd",
			countA:    0,
			countB:    4,
			inSync:    false,
			maxRounds: [4]int{2, 2, 2, 2},
			sim:       0,
		},
		{
			name:      "non-empty to empty",
			a:         "abcd",
			b:         "",
			finalA:    "abcd",
			finalB:    "abcd",
			countA:    4,
			countB:    0,
			inSync:    false,
			maxRounds: [4]int{2, 2, 2, 2},
			sim:       0,
		},
		{
			name:      "non-intersecting sets",
			a:         "ab",
			b:         "cd",
			finalA:    "abcd",
			finalB:    "abcd",
			countA:    2,
			countB:    2,
			inSync:    false,
			maxRounds: [4]int{3, 2, 2, 2},
			sim:       0,
		},
		{
			name:      "intersecting sets",
			a:         "acdefghijklmn",
			b:         "bcdopqr",
			finalA:    "abcdefghijklmnopqr",
			finalB:    "abcdefghijklmnopqr",
			countA:    13,
			countB:    7,
			inSync:    false,
			maxRounds: [4]int{4, 4, 3, 3},
			sim:       0.153,
		},
		{
			name:      "bounded reconciliation",
			a:         "acdefghijklmn",
			b:         "bcdopqr",
			finalA:    "abcdefghijklmn",
			finalB:    "abcdefgopqr",
			x:         "a",
			y:         "h",
			countA:    6,
			countB:    3,
			inSync:    false,
			maxRounds: [4]int{3, 3, 2, 2},
			sim:       0.333,
		},
		{
			name:      "bounded reconciliation with rollover",
			a:         "acdefghijklmn",
			b:         "bcdopqr",
			finalA:    "acdefghijklmnopqr",
			finalB:    "bcdhijklmnopqr",
			x:         "h",
			y:         "a",
			countA:    7,
			countB:    4,
			inSync:    false,
			maxRounds: [4]int{4, 3, 3, 2},
			sim:       0,
		},
		{
			name:      "sync against 1-element set",
			a:         "bcd",
			b:         "a",
			finalA:    "abcd",
			finalB:    "abcd",
			countA:    3,
			countB:    1,
			inSync:    false,
			maxRounds: [4]int{2, 2, 2, 2},
			sim:       0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			for n, maxSendRange := range []uint{1, 2, 3, 4} {
				t.Logf("maxSendRange: %d", maxSendRange)
				cfg := rangesync.DefaultConfig()
				cfg.MaxSendRange = maxSendRange
				cfg.ItemChunkSize = 3
				setA := makeSet(tc.a)
				syncA := rangesync.NewRangeSetReconciler(logger.Named("A"), cfg, setA)
				setB := makeSet(tc.b)
				syncB := rangesync.NewRangeSetReconciler(logger.Named("B"), cfg, setB)

				var (
					nRounds    int
					prBA, prAB rangesync.ProbeResult
				)
				var x, y rangesync.KeyBytes
				if tc.x != "" {
					x = rangesync.KeyBytes(tc.x)
					y = rangesync.KeyBytes(tc.y)
				}
				prBA = runProbe(t, syncB, syncA, x, y)
				prAB = runProbe(t, syncA, syncB, x, y)
				nRounds, _, _ = runSync(t, syncA, syncB, x, y, tc.maxRounds[n])
				t.Logf("%s: maxSendRange %d: %d rounds",
					tc.name, maxSendRange, nRounds)
				setA.AddReceived()
				setB.AddReceived()

				require.Equal(t, tc.countA, prBA.Count, "countA")
				require.Equal(t, tc.countB, prAB.Count, "countB")
				require.Equal(t, tc.inSync, prAB.InSync, "inSyncAB")
				require.Equal(t, tc.inSync, prBA.InSync, "inSyncBA")
				require.Equal(t, tc.finalA, setStr(setA), "finalA")
				require.Equal(t, tc.finalB, setStr(setB), "finalB")
				require.InDelta(t, tc.sim, prAB.Sim, 0.01, "prAB.Sim")
				require.InDelta(t, tc.sim, prBA.Sim, 0.01, "prBA.Sim")

				prBA = runProbe(t, syncB, syncA, x, y)
				prAB = runProbe(t, syncA, syncB, x, y)
				require.True(t, prAB.InSync, "inSyncAB after sync")
				require.True(t, prBA.InSync, "inSyncBA after sync")
				require.Equal(t, prAB.Count, prBA.Count, "count after sync")
				// We expect exactly 1 similarity after sync, so we don't
				// want to use require.InEpsilon as the linter suggests.
				//nolint:testifylint
				require.Equal(t, float64(1), prAB.Sim, "sim after sync")
				//nolint:testifylint
				require.Equal(t, float64(1), prBA.Sim, "sim after sync")
			}
		})
	}
}

func TestRandomSync(t *testing.T) {
	var bytesA, bytesB []byte
	defer func() {
		if t.Failed() {
			t.Logf("Random sync failed: %q <-> %q", bytesA, bytesB)
		}
	}()
	for i := 0; i < 1000; i++ {
		var chars []byte
		for c := byte(33); c < 127; c++ {
			chars = append(chars, c)
		}

		bytesA = append([]byte(nil), chars...)
		rand.Shuffle(len(bytesA), func(i, j int) {
			bytesA[i], bytesA[j] = bytesA[j], bytesA[i]
		})
		bytesA = bytesA[:rand.Intn(len(bytesA))]
		setA := makeSet(string(bytesA))

		bytesB = append([]byte(nil), chars...)
		rand.Shuffle(len(bytesB), func(i, j int) {
			bytesB[i], bytesB[j] = bytesB[j], bytesB[i]
		})
		bytesB = bytesB[:rand.Intn(len(bytesB))]
		setB := makeSet(string(bytesB))

		keySet := make(map[byte]struct{})
		for _, c := range append(bytesA, bytesB...) {
			keySet[byte(c)] = struct{}{}
		}

		expectedSet := maps.Keys(keySet)
		slices.Sort(expectedSet)

		maxSendRange := uint(rand.Intn(16) + 1)
		cfg := rangesync.DefaultConfig()
		cfg.MaxSendRange = maxSendRange
		cfg.ItemChunkSize = 3
		logger := zap.NewNop()
		syncA := rangesync.NewRangeSetReconciler(logger, cfg, setA)
		syncB := rangesync.NewRangeSetReconciler(logger, cfg, setB)

		runSync(t, syncA, syncB, nil, nil, max(len(expectedSet), 2))
		setA.AddReceived()
		setB.AddReceived()
		require.Equal(t, setStr(setA), setStr(setB))
		require.Equal(t, string(expectedSet), setStr(setA),
			"expected set for %q<->%q", bytesA, bytesB)
	}
}

type hashSyncTestConfig struct {
	maxSendRange    uint
	numTestHashes   int
	minNumSpecificA int
	maxNumSpecificA int
	minNumSpecificB int
	maxNumSpecificB int
}

type hashSyncTester struct {
	tb           testing.TB
	src          []rangesync.KeyBytes
	setA, setB   *rangesync.DumbSet
	cfg          rangesync.RangeSetReconcilerConfig
	numSpecificA int
	numSpecificB int
}

func newHashSyncTester(tb testing.TB, cfg hashSyncTestConfig) *hashSyncTester {
	tb.Helper()
	rCfg := rangesync.DefaultConfig()
	rCfg.MaxSendRange = cfg.maxSendRange
	rCfg.MaxReconcDiff = 0.1
	st := &hashSyncTester{
		tb:           tb,
		src:          make([]rangesync.KeyBytes, cfg.numTestHashes),
		cfg:          rCfg,
		numSpecificA: rand.Intn(cfg.maxNumSpecificA+1-cfg.minNumSpecificA) + cfg.minNumSpecificA,
		numSpecificB: rand.Intn(cfg.maxNumSpecificB+1-cfg.minNumSpecificB) + cfg.minNumSpecificB,
	}

	for n := range st.src {
		st.src[n] = rangesync.RandomKeyBytes(32)
	}

	sliceA := st.src[:cfg.numTestHashes-st.numSpecificB]
	st.setA = &rangesync.DumbSet{}
	for _, h := range sliceA {
		st.setA.AddUnchecked(h)
	}

	sliceB := slices.Clone(st.src[:cfg.numTestHashes-st.numSpecificB-st.numSpecificA])
	sliceB = append(sliceB, st.src[cfg.numTestHashes-st.numSpecificB:]...)
	st.setB = &rangesync.DumbSet{}
	for _, h := range sliceB {
		st.setB.AddUnchecked(h)
	}

	slices.SortFunc(st.src, func(a, b rangesync.KeyBytes) int {
		return a.Compare(b)
	})

	return st
}

func (st *hashSyncTester) verify(setA, setB rangesync.OrderedSet) {
	itemsA, err := setA.Items().Collect()
	require.NoError(st.tb, err)
	itemsB, err := setB.Items().Collect()
	require.NoError(st.tb, err)
	require.Equal(st.tb, itemsA, itemsB)
	require.Equal(st.tb, st.src, itemsA)
}

func TestSyncHash(t *testing.T) {
	st := newHashSyncTester(t, hashSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   10000,
		minNumSpecificA: 4,
		maxNumSpecificA: 90,
		minNumSpecificB: 4,
		maxNumSpecificB: 90,
	})
	logger := zap.NewNop()
	syncA := rangesync.NewRangeSetReconciler(logger, st.cfg, st.setA)
	syncB := rangesync.NewRangeSetReconciler(logger, st.cfg, st.setB)
	nRounds, nMsg, nItems := runSync(t, syncA, syncB, nil, nil, 100)
	numSpecific := st.numSpecificA + st.numSpecificB
	itemCoef := float64(nItems) / float64(numSpecific)
	t.Logf("numSpecific: %d, nRounds: %d, nMsg: %d, nItems: %d, itemCoef: %.2f",
		numSpecific, nRounds, nMsg, nItems, itemCoef)
	st.setA.AddReceived()
	st.setB.AddReceived()
	st.verify(st.setA, st.setB)
}
