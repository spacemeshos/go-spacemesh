package rangesync

import (
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

// fakeConduit is a fake Conduit for testing purposes that connects two
// RangeSetReconcilers together without any network connection.
type fakeConduit struct {
	t    *testing.T
	msgs []SyncMessage
	resp []SyncMessage
	rec  []SyncMessage
}

var _ Conduit = &fakeConduit{}

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

func (fc *fakeConduit) NextMessage() (SyncMessage, error) {
	if len(fc.msgs) != 0 {
		m := fc.msgs[0]
		fc.msgs = fc.msgs[1:]
		return m, nil
	}

	return nil, nil
}

func (fc *fakeConduit) Send(msg SyncMessage) error {
	fc.resp = append(fc.resp, msg)
	fc.rec = append(fc.rec, msg)
	return nil
}

func makeSet(t *testing.T, items string) *dumbSet {
	var s dumbSet
	for _, c := range []byte(items) {
		require.NoError(t, s.Receive(types.KeyBytes{c}))
	}
	return &s
}

func setStr(os OrderedSet) string {
	ids, err := CollectSetItems(os)
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

func dumpRangeMessages(t *testing.T, msgs []SyncMessage, fmt string, args ...any) {
	if !showMessages {
		return
	}
	t.Logf(fmt, args...)
	for _, m := range msgs {
		t.Logf("  %s", m)
	}
}

func runSync(
	t *testing.T,
	syncA, syncB *RangeSetReconciler,
	x, y types.KeyBytes,
	maxRounds int,
) (nRounds, nMsg, nItems int) {
	fc := &fakeConduit{t: t}
	require.NoError(t, syncA.Initiate(fc, x, y))
	return doRunSync(fc, syncA, syncB, maxRounds)
}

func doRunSync(fc *fakeConduit, syncA, syncB *RangeSetReconciler, maxRounds int) (nRounds, nMsg, nItems int) {
	var i int
	aDone, bDone := false, false
	dumpRangeMessages(fc.t, fc.resp, "A %q -> B %q (init):", setStr(syncA.os), setStr(syncB.os))
	dumpRangeMessages(fc.t, fc.resp, "A -> B (init):")
	for i = 0; ; i++ {
		if i == maxRounds {
			require.FailNow(fc.t, "too many rounds", "didn't reconcile in %d rounds", i)
		}
		fc.gotoResponse()
		nMsg += len(fc.msgs)
		nItems += fc.numItems()
		var err error
		bDone, err = syncB.doRound(sender{fc})
		require.NoError(fc.t, err)
		// a party should never send anything in response to the "done" message
		require.False(fc.t, aDone && !bDone, "A is done but B after that is not")
		dumpRangeMessages(fc.t, fc.resp, "B %q -> A %q:", setStr(syncA.os), setStr(syncB.os))
		dumpRangeMessages(fc.t, fc.resp, "B -> A:")
		if aDone && bDone {
			require.Empty(fc.t, fc.resp, "got messages from B in response to done msg from A")
			break
		}
		fc.gotoResponse()
		nMsg += len(fc.msgs)
		nItems += fc.numItems()
		aDone, err = syncA.doRound(sender{fc})
		require.NoError(fc.t, err)
		dumpRangeMessages(fc.t, fc.msgs, "A %q --> B %q:", setStr(syncB.os), setStr(syncA.os))
		dumpRangeMessages(fc.t, fc.resp, "A -> B:")
		require.False(fc.t, bDone && !aDone, "B is done but A after that is not")
		if aDone && bDone {
			require.Empty(fc.t, fc.resp, "got messages from A in response to done msg from B")
			break
		}
	}
	return i + 1, nMsg, nItems
}

func runProbe(t *testing.T, from, to *RangeSetReconciler, x, y types.KeyBytes) ProbeResult {
	fc := &fakeConduit{t: t}
	info, err := from.InitiateProbe(fc, x, y)
	require.NoError(t, err)
	return doRunProbe(fc, from, to, info)
}

func doRunProbe(fc *fakeConduit, from, to *RangeSetReconciler, info RangeInfo) ProbeResult {
	require.NotEmpty(fc.t, fc.resp, "empty initial round")
	fc.gotoResponse()
	done, err := to.doRound(sender{fc})
	require.True(fc.t, done)
	require.NoError(fc.t, err)
	fc.gotoResponse()
	pr, err := from.HandleProbeResponse(fc, info)
	require.NoError(fc.t, err)
	require.Nil(fc.t, fc.resp, "got messages from Probe in response to done msg")
	return pr
}

func TestRangeSync(t *testing.T) {
	for _, tc := range []struct {
		name           string
		a, b           string
		finalA, finalB string
		x, y           string
		countA, countB int
		fpA, fpB       types.Fingerprint
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
			fpA:       types.EmptyFingerprint(),
			fpB:       types.EmptyFingerprint(),
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
			fpA:       types.EmptyFingerprint(),
			fpB:       stringToFP("abcd"),
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
			fpA:       stringToFP("abcd"),
			fpB:       types.EmptyFingerprint(),
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
			fpA:       stringToFP("ab"),
			fpB:       stringToFP("cd"),
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
			fpA:       stringToFP("acdefghijklmn"),
			fpB:       stringToFP("bcdopqr"),
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
			fpA:       stringToFP("acdefg"),
			fpB:       stringToFP("bcd"),
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
			fpA:       stringToFP("hijklmn"),
			fpB:       stringToFP("opqr"),
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
			fpA:       stringToFP("bcd"),
			fpB:       stringToFP("a"),
			maxRounds: [4]int{2, 2, 2, 2},
			sim:       0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			for n, maxSendRange := range []int{1, 2, 3, 4} {
				t.Logf("maxSendRange: %d", maxSendRange)
				setA := makeSet(t, tc.a)
				setA.disableReAdd = true
				syncA := NewRangeSetReconciler(setA,
					WithLogger(logger.Named("A")),
					WithMaxSendRange(maxSendRange),
					WithItemChunkSize(3))
				setB := makeSet(t, tc.b)
				setB.disableReAdd = true
				syncB := NewRangeSetReconciler(setB,
					WithLogger(logger.Named("B")),
					WithMaxSendRange(maxSendRange),
					WithItemChunkSize(3))

				var (
					nRounds    int
					prBA, prAB ProbeResult
				)
				var x, y types.KeyBytes
				if tc.x != "" {
					x = types.KeyBytes(tc.x)
					y = types.KeyBytes(tc.y)
				}
				prBA = runProbe(t, syncB, syncA, x, y)
				prAB = runProbe(t, syncA, syncB, x, y)
				nRounds, _, _ = runSync(t, syncA, syncB, x, y, tc.maxRounds[n])
				t.Logf("%s: maxSendRange %d: %d rounds",
					tc.name, maxSendRange, nRounds)

				require.Equal(t, tc.countA, prBA.Count, "countA")
				require.Equal(t, tc.countB, prAB.Count, "countB")
				require.Equal(t, tc.fpA, prBA.FP, "fpA")
				require.Equal(t, tc.fpB, prAB.FP, "fpB")
				require.Equal(t, tc.finalA, setStr(setA), "finalA")
				require.Equal(t, tc.finalB, setStr(setB), "finalB")
				require.InDelta(t, tc.sim, prAB.Sim, 0.01, "prAB.Sim")
				require.InDelta(t, tc.sim, prBA.Sim, 0.01, "prBA.Sim")
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
		setA := makeSet(t, string(bytesA))

		bytesB = append([]byte(nil), chars...)
		rand.Shuffle(len(bytesB), func(i, j int) {
			bytesB[i], bytesB[j] = bytesB[j], bytesB[i]
		})
		bytesB = bytesB[:rand.Intn(len(bytesB))]
		setB := makeSet(t, string(bytesB))

		keySet := make(map[byte]struct{})
		for _, c := range append(bytesA, bytesB...) {
			keySet[byte(c)] = struct{}{}
		}

		expectedSet := maps.Keys(keySet)
		slices.Sort(expectedSet)

		maxSendRange := rand.Intn(16) + 1
		syncA := NewRangeSetReconciler(setA,
			WithMaxSendRange(maxSendRange),
			WithItemChunkSize(3))
		syncB := NewRangeSetReconciler(setB,
			WithMaxSendRange(maxSendRange),
			WithItemChunkSize(3))

		runSync(t, syncA, syncB, nil, nil, max(len(expectedSet), 2))
		// t.Logf("maxSendRange %d a %d b %d n %d", maxSendRange, len(bytesA), len(bytesB), n)
		require.Equal(t, setStr(setA), setStr(setB))
		require.Equal(t, string(expectedSet), setStr(setA),
			"expected set for %q<->%q", bytesA, bytesB)
	}
}

type hashSyncTestConfig struct {
	maxSendRange    int
	numTestHashes   int
	minNumSpecificA int
	maxNumSpecificA int
	minNumSpecificB int
	maxNumSpecificB int
	allowReAdd      bool
}

type hashSyncTester struct {
	t            *testing.T
	src          []types.KeyBytes
	setA, setB   OrderedSet
	opts         []RangeSetReconcilerOption
	numSpecificA int
	numSpecificB int
}

func newHashSyncTester(t *testing.T, cfg hashSyncTestConfig) *hashSyncTester {
	st := &hashSyncTester{
		t:   t,
		src: make([]types.KeyBytes, cfg.numTestHashes),
		opts: []RangeSetReconcilerOption{
			WithMaxSendRange(cfg.maxSendRange),
			WithMaxDiff(0.1),
		},
		numSpecificA: rand.Intn(cfg.maxNumSpecificA+1-cfg.minNumSpecificA) + cfg.minNumSpecificA,
		numSpecificB: rand.Intn(cfg.maxNumSpecificB+1-cfg.minNumSpecificB) + cfg.minNumSpecificB,
	}

	for n := range st.src {
		st.src[n] = types.RandomKeyBytes(32)
	}

	sliceA := st.src[:cfg.numTestHashes-st.numSpecificB]
	st.setA = NewDumbHashSet(!cfg.allowReAdd)
	for _, h := range sliceA {
		require.NoError(t, st.setA.Receive(h))
	}

	sliceB := slices.Clone(st.src[:cfg.numTestHashes-st.numSpecificB-st.numSpecificA])
	sliceB = append(sliceB, st.src[cfg.numTestHashes-st.numSpecificB:]...)
	st.setB = NewDumbHashSet(!cfg.allowReAdd)
	for _, h := range sliceB {
		require.NoError(t, st.setB.Receive(h))
	}

	slices.SortFunc(st.src, func(a, b types.KeyBytes) int {
		return a.Compare(b)
	})

	return st
}

func (st *hashSyncTester) verify(setA, setB OrderedSet) {
	itemsA, err := CollectSetItems(setA)
	require.NoError(st.t, err)
	itemsB, err := CollectSetItems(setB)
	require.NoError(st.t, err)
	require.Equal(st.t, itemsA, itemsB)
	require.Equal(st.t, st.src, itemsA)
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
	syncA := NewRangeSetReconciler(st.setA, st.opts...)
	syncB := NewRangeSetReconciler(st.setB, st.opts...)
	nRounds, nMsg, nItems := runSync(t, syncA, syncB, nil, nil, 100)
	numSpecific := st.numSpecificA + st.numSpecificB
	itemCoef := float64(nItems) / float64(numSpecific)
	t.Logf("numSpecific: %d, nRounds: %d, nMsg: %d, nItems: %d, itemCoef: %.2f",
		numSpecific, nRounds, nMsg, nItems, itemCoef)
	st.verify(st.setA, st.setB)
}

func TestDeferredAdd(t *testing.T) {
	st := newHashSyncTester(t, hashSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   10000,
		minNumSpecificA: 4,
		maxNumSpecificA: 90,
		minNumSpecificB: 4,
		maxNumSpecificB: 90,
	})
	opts := append(st.opts, WithMaxDiff(0.9))
	var msgLists [][]SyncMessage

	sync := func(setA, setB OrderedSet) {
		syncA := NewRangeSetReconciler(setA, opts...)
		syncB := NewRangeSetReconciler(setB, opts...)
		fc := &fakeConduit{t: t}
		require.NoError(t, syncA.Initiate(fc, nil, nil))
		nRounds, nMsg, nItems := doRunSync(fc, syncA, syncB, 100)
		numSpecific := st.numSpecificA + st.numSpecificB
		itemCoef := float64(nItems) / float64(numSpecific)
		t.Logf("numSpecific: %d, nRounds: %d, nMsg: %d, nItems: %d, itemCoef: %.2f",
			numSpecific, nRounds, nMsg, nItems, itemCoef)
		msgLists = append(msgLists, fc.rec)
	}

	setA := st.setA.Copy(true)
	setB := st.setB.Copy(true)
	sync(setA, setB)
	st.verify(setA, setB)

	dSetA := &deferredAddSet{OrderedSet: st.setA.Copy(true)}
	dSetB := &deferredAddSet{OrderedSet: st.setB.Copy(true)}
	sync(dSetA, dSetB)
	dSetA.addAll()
	dSetB.addAll()
	st.verify(dSetA, dSetB)

	require.Equal(t, msgLists[0], msgLists[1])
}
