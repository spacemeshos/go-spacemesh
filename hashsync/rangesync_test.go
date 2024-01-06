package hashsync

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
)

type rangeMessage struct {
	x, y      Ordered
	fp        any
	count     int
	haveItems bool
}

func (m rangeMessage) X() Ordered       { return m.x }
func (m rangeMessage) Y() Ordered       { return m.y }
func (m rangeMessage) Fingerprint() any { return m.fp }
func (m rangeMessage) Count() int       { return m.count }
func (m rangeMessage) HaveItems() bool  { return m.haveItems }

var _ SyncMessage = rangeMessage{}

func (m rangeMessage) String() string {
	itemsStr := ""
	if m.haveItems {
		itemsStr = fmt.Sprintf(" +items")
	}
	return fmt.Sprintf("<X %v Y %v Count %d Fingerprint %v%s>",
		m.x, m.y, m.count, m.fp, itemsStr)
}

type fakeConduit struct {
	t     *testing.T
	msgs  []rangeMessage
	items []Ordered
	resp  *fakeConduit
}

var _ Conduit = &fakeConduit{}

func (fc *fakeConduit) done() bool {
	if fc.resp == nil {
		return true
	}
	return len(fc.resp.msgs) == 0 && len(fc.resp.items) == 0
}

func (fc *fakeConduit) NextMessage() (SyncMessage, error) {
	if len(fc.msgs) != 0 {
		m := fc.msgs[0]
		fc.msgs = fc.msgs[1:]
		return m, nil
	}

	return nil, nil
}

func (fc *fakeConduit) NextItem() (Ordered, error) {
	if len(fc.items) != 0 {
		item := fc.items[0]
		fc.items = fc.items[1:]
		return item, nil
	}

	return nil, nil
}

func (fc *fakeConduit) ensureResp() {
	if fc.resp == nil {
		fc.resp = &fakeConduit{t: fc.t}
	}
}

func (fc *fakeConduit) sendMsg(x, y Ordered, fingerprint any, count int, haveItems bool) {
	fc.ensureResp()
	msg := rangeMessage{
		x:         x,
		y:         y,
		fp:        fingerprint,
		count:     count,
		haveItems: haveItems,
	}
	fc.resp.msgs = append(fc.resp.msgs, msg)
}

func (fc *fakeConduit) sendItems(count int, it Iterator) {
	require.NotZero(fc.t, count)
	require.NotNil(fc.t, it)
	fc.ensureResp()
	for i := 0; i < count; i++ {
		if it.Key() == nil {
			panic("fakeConduit.SendItems: went got to the end of the tree")
		}
		fc.resp.items = append(fc.resp.items, it.Key())
		it.Next()
	}
}

func (fc *fakeConduit) SendFingerprint(x, y Ordered, fingerprint any, count int) {
	require.NotNil(fc.t, x)
	require.NotNil(fc.t, y)
	require.NotZero(fc.t, count)
	require.NotNil(fc.t, fingerprint)
	fc.sendMsg(x, y, fingerprint, count, false)
}

func (fc *fakeConduit) SendEmptySet() {
	fc.sendMsg(nil, nil, nil, 0, false)
}

func (fc *fakeConduit) SendEmptyRange(x, y Ordered) {
	require.NotNil(fc.t, x)
	require.NotNil(fc.t, y)
	fc.sendMsg(x, y, nil, 0, false)
}

func (fc *fakeConduit) SendItems(x, y Ordered, count int, it Iterator) {
	require.Positive(fc.t, count)
	require.NotNil(fc.t, x)
	require.NotNil(fc.t, y)
	fc.sendMsg(x, y, nil, count, true)
	fc.sendItems(count, it)
}

func (fc *fakeConduit) SendItemsOnly(count int, it Iterator) {
	fc.sendItems(count, it)
}

type dumbStoreIterator struct {
	ds *dumbStore
	n  int
}

var _ Iterator = &dumbStoreIterator{}

func (it *dumbStoreIterator) Equal(other Iterator) bool {
	o := other.(*dumbStoreIterator)
	if it.ds != o.ds {
		panic("comparing iterators from different dumbStores")
	}
	return it.n == o.n
}

func (it *dumbStoreIterator) Key() Ordered {
	return it.ds.items[it.n]
}

func (it *dumbStoreIterator) Next() {
	if len(it.ds.items) != 0 {
		it.n = (it.n + 1) % len(it.ds.items)
	}
}

type dumbStore struct {
	items []sampleID
}

var _ ItemStore = &dumbStore{}

func (ds *dumbStore) Add(k Ordered) {
	id := k.(sampleID)
	if len(ds.items) == 0 {
		ds.items = []sampleID{id}
		return
	}
	p := slices.IndexFunc(ds.items, func(other sampleID) bool {
		return other >= id
	})
	switch {
	case p < 0:
		ds.items = append(ds.items, id)
	case id == ds.items[p]:
		// already present
	default:
		ds.items = slices.Insert(ds.items, p, id)
	}
}

func (ds *dumbStore) iter(n int) Iterator {
	if n == -1 || n == len(ds.items) {
		return nil
	}
	return &dumbStoreIterator{ds: ds, n: n}
}

func (ds *dumbStore) last() sampleID {
	if len(ds.items) == 0 {
		panic("can't get the last element: zero items")
	}
	return ds.items[len(ds.items)-1]
}

func (ds *dumbStore) iterFor(s sampleID) Iterator {
	n := slices.Index(ds.items, s)
	if n == -1 {
		panic("item not found: " + s)
	}
	return ds.iter(n)
}

func (ds *dumbStore) GetRangeInfo(preceding Iterator, x, y Ordered, count int) RangeInfo {
	all := storeItemStr(ds)
	vx := x.(sampleID)
	vy := y.(sampleID)
	if preceding != nil && preceding.Key().Compare(x) > 0 {
		panic("preceding info after x")
	}
	fp, startStr, endStr := naiveRange(all, string(vx), string(vy), count)
	r := RangeInfo{
		Fingerprint: fp,
		Count:       len(fp),
	}
	if all != "" {
		if startStr == "" || endStr == "" {
			panic("empty startStr/endStr from naiveRange")
		}
		r.Start = ds.iterFor(sampleID(startStr))
		r.End = ds.iterFor(sampleID(endStr))
	}
	return r
}

func (ds *dumbStore) Min() Iterator {
	if len(ds.items) == 0 {
		return nil
	}
	return &dumbStoreIterator{
		ds: ds,
		n:  0,
	}
}

func (ds *dumbStore) Max() Iterator {
	if len(ds.items) == 0 {
		return nil
	}
	return &dumbStoreIterator{
		ds: ds,
		n:  len(ds.items) - 1,
	}
}

type verifiedStoreIterator struct {
	t         *testing.T
	knownGood Iterator
	it        Iterator
}

var _ Iterator = &verifiedStoreIterator{}

func (it verifiedStoreIterator) Equal(other Iterator) bool {
	o := other.(verifiedStoreIterator)
	eq1 := it.knownGood.Equal(o.knownGood)
	eq2 := it.it.Equal(o.it)
	assert.Equal(it.t, eq1, eq2, "iterators equal -- keys <%v> <%v> / <%v> <%v>",
		it.knownGood.Key(), it.it.Key(),
		o.knownGood.Key(), o.it.Key())
	assert.Equal(it.t, it.knownGood.Key(), it.it.Key(), "keys of equal iterators")
	return eq2
}

func (it verifiedStoreIterator) Key() Ordered {
	k1 := it.knownGood.Key()
	k2 := it.it.Key()
	assert.Equal(it.t, k1, k2, "keys")
	return k2
}

func (it verifiedStoreIterator) Next() {
	it.knownGood.Next()
	it.it.Next()
	assert.Equal(it.t, it.knownGood.Key(), it.it.Key(), "keys for Next()")
}

type verifiedStore struct {
	t            *testing.T
	knownGood    ItemStore
	store        ItemStore
	disableReAdd bool
	added        map[sampleID]struct{}
}

var _ ItemStore = &verifiedStore{}

func disableReAdd(s ItemStore) {
	if vs, ok := s.(*verifiedStore); ok {
		vs.disableReAdd = true
	}
}

func (vs *verifiedStore) Add(k Ordered) {
	if vs.disableReAdd {
		_, found := vs.added[k.(sampleID)]
		require.False(vs.t, found, "hash sent twice: %v", k)
		if vs.added == nil {
			vs.added = make(map[sampleID]struct{})
		}
		vs.added[k.(sampleID)] = struct{}{}
	}
	vs.knownGood.Add(k)
	vs.store.Add(k)
}

func (vs *verifiedStore) GetRangeInfo(preceding Iterator, x, y Ordered, count int) RangeInfo {
	var ri1, ri2 RangeInfo
	if preceding != nil {
		p := preceding.(verifiedStoreIterator)
		ri1 = vs.knownGood.GetRangeInfo(p.knownGood, x, y, count)
		ri2 = vs.store.GetRangeInfo(p.it, x, y, count)
	} else {
		ri1 = vs.knownGood.GetRangeInfo(nil, x, y, count)
		ri2 = vs.store.GetRangeInfo(nil, x, y, count)
	}
	require.Equal(vs.t, ri1.Fingerprint, ri2.Fingerprint, "range info fingerprint")
	require.Equal(vs.t, ri1.Count, ri2.Count, "range info count")
	ri := RangeInfo{
		Fingerprint: ri2.Fingerprint,
		Count:       ri2.Count,
	}
	if ri1.Start == nil {
		require.Nil(vs.t, ri2.Start, "range info start")
		require.Nil(vs.t, ri1.End, "range info end (known good)")
		require.Nil(vs.t, ri2.End, "range info end")
	} else {
		require.NotNil(vs.t, ri2.Start, "range info start")
		require.Equal(vs.t, ri1.Start.Key(), ri2.Start.Key(), "range info start key")
		require.NotNil(vs.t, ri1.End, "range info end (known good)")
		require.NotNil(vs.t, ri2.End, "range info end")
		ri.Start = verifiedStoreIterator{
			t:         vs.t,
			knownGood: ri1.Start,
			it:        ri2.Start,
		}
	}
	if ri1.End == nil {
		require.Nil(vs.t, ri2.End, "range info end")
	} else {
		require.NotNil(vs.t, ri2.End, "range info end")
		require.Equal(vs.t, ri1.End.Key(), ri2.End.Key(), "range info end key")
		ri.End = verifiedStoreIterator{
			t:         vs.t,
			knownGood: ri1.End,
			it:        ri2.End,
		}
	}
	// QQQQQ: TODO: if count >= 0 and start+end != nil, do more calls to GetRangeInfo using resulting
	// end iterator key to make sure the range is correct
	return ri
}

func (vs *verifiedStore) Min() Iterator {
	m1 := vs.knownGood.Min()
	m2 := vs.knownGood.Min()
	if m1 == nil {
		require.Nil(vs.t, m2, "Min")
		return nil
	} else {
		require.NotNil(vs.t, m2, "Min")
		require.Equal(vs.t, m1.Key(), m2.Key(), "Min key")
	}
	return verifiedStoreIterator{
		t:         vs.t,
		knownGood: m1,
		it:        m2,
	}
}

func (vs *verifiedStore) Max() Iterator {
	m1 := vs.knownGood.Max()
	m2 := vs.knownGood.Max()
	if m1 == nil {
		require.Nil(vs.t, m2, "Max")
		return nil
	} else {
		require.NotNil(vs.t, m2, "Max")
		require.Equal(vs.t, m1.Key(), m2.Key(), "Max key")
	}
	return verifiedStoreIterator{
		t:         vs.t,
		knownGood: m1,
		it:        m2,
	}
}

type storeFactory func(t *testing.T) ItemStore

func makeDumbStore(t *testing.T) ItemStore {
	return &dumbStore{}
}

func makeMonoidTreeStore(t *testing.T) ItemStore {
	return NewMonoidTreeStore(sampleMonoid{})
}

func makeVerifiedMonoidTreeStore(t *testing.T) ItemStore {
	return &verifiedStore{
		t:         t,
		knownGood: makeDumbStore(t),
		store:     makeMonoidTreeStore(t),
	}
}

func makeStore(t *testing.T, f storeFactory, items string) ItemStore {
	s := f(t)
	for _, c := range items {
		s.Add(sampleID(c))
	}
	return s
}

func storeItemStr(is ItemStore) string {
	it := is.Min()
	if it == nil {
		return ""
	}
	endAt := is.Min()
	r := ""
	for {
		r += string(it.Key().(sampleID))
		it.Next()
		if it.Equal(endAt) {
			return r
		}
	}
}

var testStores = []struct {
	name    string
	factory storeFactory
}{
	{
		name:    "dumb store",
		factory: makeDumbStore,
	},
	{
		name:    "monoid tree store",
		factory: makeMonoidTreeStore,
	},
	{
		name:    "verified monoid tree store",
		factory: makeVerifiedMonoidTreeStore,
	},
}

func forTestStores(t *testing.T, testFunc func(t *testing.T, factory storeFactory)) {
	for _, s := range testStores {
		t.Run(s.name, func(t *testing.T) {
			testFunc(t, s.factory)
		})
	}
}

// QQQQQ: rm
func dumpRangeMessages(t *testing.T, msgs []rangeMessage, fmt string, args ...any) {
	t.Logf(fmt, args...)
	for _, m := range msgs {
		t.Logf("  %s", m)
	}
}

func runSync(t *testing.T, syncA, syncB *RangeSetReconciler, maxRounds int) (nRounds, nMsg, nItems int) {
	fc := &fakeConduit{t: t}
	syncA.Initiate(fc)
	require.False(t, fc.done(), "no messages from Initiate")
	var i int
	for i = 0; !fc.done(); i++ {
		if i == maxRounds {
			require.FailNow(t, "too many rounds", "didn't reconcile in %d rounds", i)
		}
		fc = fc.resp
		// dumpRangeMessages(t, fc.msgs, "A %q -> B %q:", storeItemStr(syncA.is), storeItemStr(syncB.is))
		nMsg += len(fc.msgs)
		nItems += len(fc.items)
		syncB.Process(fc)
		if fc.done() {
			break
		}
		fc = fc.resp
		nMsg += len(fc.msgs)
		nItems += len(fc.items)
		// dumpRangeMessages(t, fc.msgs, "B %q --> A %q:", storeItemStr(syncB.is), storeItemStr(syncA.is))
		syncA.Process(fc)
	}
	return i + 1, nMsg, nItems
}

func testRangeSync(t *testing.T, storeFactory storeFactory) {
	for _, tc := range []struct {
		name      string
		a, b      string
		final     string
		maxRounds [4]int
	}{
		{
			name:      "empty sets",
			a:         "",
			b:         "",
			final:     "",
			maxRounds: [4]int{1, 1, 1, 1},
		},
		{
			name:      "empty to non-empty",
			a:         "",
			b:         "abcd",
			final:     "abcd",
			maxRounds: [4]int{1, 1, 1, 1},
		},
		{
			name:      "non-empty to empty",
			a:         "abcd",
			b:         "",
			final:     "abcd",
			maxRounds: [4]int{2, 2, 2, 2},
		},
		{
			name:      "non-intersecting sets",
			a:         "ab",
			b:         "cd",
			final:     "abcd",
			maxRounds: [4]int{3, 2, 2, 2},
		},
		{
			name:      "intersecting sets",
			a:         "acdefghijklmn",
			b:         "bcdopqr",
			final:     "abcdefghijklmnopqr",
			maxRounds: [4]int{4, 4, 4, 3},
		},
		{
			name:      "sync against 1-element set",
			a:         "bcd",
			b:         "a",
			final:     "abcd",
			maxRounds: [4]int{3, 2, 2, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for n, maxSendRange := range []int{1, 2, 3, 4} {
				t.Logf("maxSendRange: %d", maxSendRange)
				storeA := makeStore(t, storeFactory, tc.a)
				disableReAdd(storeA)
				syncA := NewRangeSetReconciler(storeA, WithMaxSendRange(maxSendRange))
				storeB := makeStore(t, storeFactory, tc.b)
				disableReAdd(storeB)
				syncB := NewRangeSetReconciler(storeB, WithMaxSendRange(maxSendRange))

				nRounds, _, _ := runSync(t, syncA, syncB, tc.maxRounds[n])
				t.Logf("%s: maxSendRange %d: %d rounds", tc.name, maxSendRange, nRounds)

				require.Equal(t, storeItemStr(storeA), storeItemStr(storeB))
				require.Equal(t, tc.final, storeItemStr(storeA))
			}
		})
	}
}

func TestRangeSync(t *testing.T) {
	forTestStores(t, testRangeSync)
}

func testRandomSync(t *testing.T, storeFactory storeFactory) {
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
		storeA := makeStore(t, storeFactory, string(bytesA))

		bytesB = append([]byte(nil), chars...)
		rand.Shuffle(len(bytesB), func(i, j int) {
			bytesB[i], bytesB[j] = bytesB[j], bytesB[i]
		})
		bytesB = bytesB[:rand.Intn(len(bytesB))]
		storeB := makeStore(t, storeFactory, string(bytesB))

		keySet := make(map[byte]struct{})
		for _, c := range append(bytesA, bytesB...) {
			keySet[byte(c)] = struct{}{}
		}

		expectedSet := maps.Keys(keySet)
		slices.Sort(expectedSet)

		maxSendRange := rand.Intn(16) + 1
		syncA := NewRangeSetReconciler(storeA, WithMaxSendRange(maxSendRange))
		syncB := NewRangeSetReconciler(storeB, WithMaxSendRange(maxSendRange))

		runSync(t, syncA, syncB, max(len(expectedSet), 2)) // FIXME: less rounds!
		// t.Logf("maxSendRange %d a %d b %d n %d", maxSendRange, len(bytesA), len(bytesB), n)
		require.Equal(t, storeItemStr(storeA), storeItemStr(storeB))
		require.Equal(t, string(expectedSet), storeItemStr(storeA),
			"expected set for %q<->%q", bytesA, bytesB)
	}
}

func TestRandomSync(t *testing.T) {
	forTestStores(t, testRandomSync)
}

// TBD: test XOR + big sync
// TBD: include initiate round!!!
// TBD: use logger for verbose logging (messages)
// TBD: in fakeConduit -- check item count against the iterator in SendItems / SendItemsOnly!!
// TBD: record interaction using golden master in testRangeSync, together with N of rounds / msgs / items and don't check max rounds
