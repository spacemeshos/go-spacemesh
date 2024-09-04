package rangesync

import (
	"cmp"
	"fmt"
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

//go:generate scalegen

type Marker struct{}

func (*Marker) X() types.Ordered               { return nil }
func (*Marker) Y() types.Ordered               { return nil }
func (*Marker) Fingerprint() types.Fingerprint { return types.EmptyFingerprint() }
func (*Marker) Count() int                     { return 0 }
func (*Marker) Keys() []types.Ordered          { return nil }
func (*Marker) Since() time.Time               { return time.Time{} }

// DoneMessage is a SyncMessage that denotes the end of the synchronization.
// The peer should stop any further processing after receiving this message.
type DoneMessage struct{ Marker }

var _ SyncMessage = &DoneMessage{}

func (*DoneMessage) Type() MessageType { return MessageTypeDone }

// EndRoundMessage is a SyncMessage that denotes the end of the sync round.
type EndRoundMessage struct{ Marker }

var _ SyncMessage = &EndRoundMessage{}

func (*EndRoundMessage) Type() MessageType { return MessageTypeEndRound }

// EmptySetMessage is a SyncMessage that denotes an empty set, requesting the
// peer to send all of its items
type EmptySetMessage struct{ Marker }

var _ SyncMessage = &EmptySetMessage{}

func (*EmptySetMessage) Type() MessageType { return MessageTypeEmptySet }

// EmptyRangeMessage notifies the peer that it needs to send all of its items in
// the specified range
type EmptyRangeMessage struct {
	RangeX, RangeY CompactHash
}

var _ SyncMessage = &EmptyRangeMessage{}

func (m *EmptyRangeMessage) Type() MessageType              { return MessageTypeEmptyRange }
func (m *EmptyRangeMessage) X() types.Ordered               { return m.RangeX.ToOrdered() }
func (m *EmptyRangeMessage) Y() types.Ordered               { return m.RangeY.ToOrdered() }
func (m *EmptyRangeMessage) Fingerprint() types.Fingerprint { return types.EmptyFingerprint() }
func (m *EmptyRangeMessage) Count() int                     { return 0 }
func (m *EmptyRangeMessage) Keys() []types.Ordered          { return nil }
func (m *EmptyRangeMessage) Since() time.Time               { return time.Time{} }

// FingerprintMessage contains range fingerprint for comparison against the
// peer's fingerprint of the range with the same bounds [RangeX, RangeY)
type FingerprintMessage struct {
	RangeX, RangeY   CompactHash
	RangeFingerprint types.Fingerprint
	NumItems         uint32
}

var _ SyncMessage = &FingerprintMessage{}

func (m *FingerprintMessage) Type() MessageType              { return MessageTypeFingerprint }
func (m *FingerprintMessage) X() types.Ordered               { return m.RangeX.ToOrdered() }
func (m *FingerprintMessage) Y() types.Ordered               { return m.RangeY.ToOrdered() }
func (m *FingerprintMessage) Fingerprint() types.Fingerprint { return m.RangeFingerprint }
func (m *FingerprintMessage) Count() int                     { return int(m.NumItems) }
func (m *FingerprintMessage) Keys() []types.Ordered          { return nil }
func (m *FingerprintMessage) Since() time.Time               { return time.Time{} }

// RangeContentsMessage denotes a range for which the set of items has been sent.
// The peer needs to send back any items it has in the same range bounded
// by [RangeX, RangeY)
type RangeContentsMessage struct {
	RangeX, RangeY CompactHash
	NumItems       uint32
}

var _ SyncMessage = &RangeContentsMessage{}

func (m *RangeContentsMessage) Type() MessageType              { return MessageTypeRangeContents }
func (m *RangeContentsMessage) X() types.Ordered               { return m.RangeX.ToOrdered() }
func (m *RangeContentsMessage) Y() types.Ordered               { return m.RangeY.ToOrdered() }
func (m *RangeContentsMessage) Fingerprint() types.Fingerprint { return types.EmptyFingerprint() }
func (m *RangeContentsMessage) Count() int                     { return int(m.NumItems) }
func (m *RangeContentsMessage) Keys() []types.Ordered          { return nil }
func (m *RangeContentsMessage) Since() time.Time               { return time.Time{} }

// ItemBatchMessage denotes a batch of items to be added to the peer's set.
type ItemBatchMessage struct {
	ContentKeys KeyCollection `scale:"max=1024"`
}

func (m *ItemBatchMessage) Type() MessageType              { return MessageTypeItemBatch }
func (m *ItemBatchMessage) X() types.Ordered               { return nil }
func (m *ItemBatchMessage) Y() types.Ordered               { return nil }
func (m *ItemBatchMessage) Fingerprint() types.Fingerprint { return types.EmptyFingerprint() }
func (m *ItemBatchMessage) Count() int                     { return 0 }
func (m *ItemBatchMessage) Keys() []types.Ordered {
	var r []types.Ordered
	for _, k := range m.ContentKeys.Keys {
		r = append(r, k)
	}
	return r
}
func (m *ItemBatchMessage) Since() time.Time { return time.Time{} }

// ProbeMessage requests bounded range fingerprint and count from the peer,
// along with a minhash sample if fingerprints differ
type ProbeMessage struct {
	RangeX, RangeY   CompactHash
	RangeFingerprint types.Fingerprint
	SampleSize       uint32
}

var _ SyncMessage = &ProbeMessage{}

func (m *ProbeMessage) Type() MessageType              { return MessageTypeProbe }
func (m *ProbeMessage) X() types.Ordered               { return m.RangeX.ToOrdered() }
func (m *ProbeMessage) Y() types.Ordered               { return m.RangeY.ToOrdered() }
func (m *ProbeMessage) Fingerprint() types.Fingerprint { return m.RangeFingerprint }
func (m *ProbeMessage) Count() int                     { return int(m.SampleSize) }
func (m *ProbeMessage) Keys() []types.Ordered          { return nil }
func (m *ProbeMessage) Since() time.Time               { return time.Time{} }

// MinhashSampleItem represents an item of minhash sample subset
type MinhashSampleItem uint32

var _ types.Ordered = MinhashSampleItem(0)

func (m MinhashSampleItem) String() string {
	return fmt.Sprintf("0x%08x", uint32(m))
}

// Compare implements types.Ordered
func (m MinhashSampleItem) Compare(other any) int {
	return cmp.Compare(m, other.(MinhashSampleItem))
}

// EncodeScale implements scale.Encodable.
func (m MinhashSampleItem) EncodeScale(e *scale.Encoder) (int, error) {
	// FIXME: there's EncodeUint32 (non-compact which is better for hashes) but no
	// DecodeUint32
	return scale.EncodeCompact32(e, uint32(m))
}

// DecodeScale implements scale.Decodable.
func (m *MinhashSampleItem) DecodeScale(d *scale.Decoder) (int, error) {
	v, total, err := scale.DecodeCompact32(d)
	*m = MinhashSampleItem(v)
	return total, err
}

// MinhashSampleItemFromKeyBytes uses lower 32 bits of a hash as a MinhashSampleItem
func MinhashSampleItemFromKeyBytes(h types.KeyBytes) MinhashSampleItem {
	if len(h) < 4 {
		panic("BUG: unexpected hash size")
	}
	l := len(h)
	return MinhashSampleItem(uint32(h[l-4])<<24 + uint32(h[l-3])<<16 + uint32(h[l-2])<<8 + uint32(h[l-1]))
}

// SampleMessage is a sample of set items
type SampleMessage struct {
	RangeX, RangeY   CompactHash
	RangeFingerprint types.Fingerprint
	NumItems         uint32
	// NOTE: max must be in sync with maxSampleSize in hashsync/rangesync.go
	Sample []MinhashSampleItem `scale:"max=1000"`
}

var _ SyncMessage = &SampleMessage{}

func (m *SampleMessage) Type() MessageType              { return MessageTypeSample }
func (m *SampleMessage) X() types.Ordered               { return m.RangeX.ToOrdered() }
func (m *SampleMessage) Y() types.Ordered               { return m.RangeY.ToOrdered() }
func (m *SampleMessage) Fingerprint() types.Fingerprint { return m.RangeFingerprint }
func (m *SampleMessage) Count() int                     { return int(m.NumItems) }
func (m *SampleMessage) Keys() []types.Ordered {
	r := make([]types.Ordered, len(m.Sample))
	for n, item := range m.Sample {
		r[n] = item
	}
	return r
}
func (m *SampleMessage) Since() time.Time { return time.Time{} }

// RecentMessage is a SyncMessage that denotes a set of items that have been
// added to the peer's set since the specific point in time.
type RecentMessage struct {
	SinceTime uint64
}

var _ SyncMessage = &RecentMessage{}

func (m *RecentMessage) Type() MessageType              { return MessageTypeRecent }
func (m *RecentMessage) X() types.Ordered               { return nil }
func (m *RecentMessage) Y() types.Ordered               { return nil }
func (m *RecentMessage) Fingerprint() types.Fingerprint { return types.EmptyFingerprint() }
func (m *RecentMessage) Count() int                     { return 0 }
func (m *RecentMessage) Keys() []types.Ordered          { return nil }
func (m *RecentMessage) Since() time.Time               { return time.Unix(0, int64(m.SinceTime)) }

// TODO: don't do scalegen for empty types
