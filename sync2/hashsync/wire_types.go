package hashsync

import (
	"cmp"
	"fmt"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

type Marker struct{}

func (*Marker) X() Ordered       { return nil }
func (*Marker) Y() Ordered       { return nil }
func (*Marker) Fingerprint() any { return nil }
func (*Marker) Count() int       { return 0 }
func (*Marker) Keys() []Ordered  { return nil }

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
	RangeX, RangeY CompactHash32
}

var _ SyncMessage = &EmptyRangeMessage{}

func (m *EmptyRangeMessage) Type() MessageType { return MessageTypeEmptyRange }
func (m *EmptyRangeMessage) X() Ordered        { return m.RangeX.ToOrdered() }
func (m *EmptyRangeMessage) Y() Ordered        { return m.RangeY.ToOrdered() }
func (m *EmptyRangeMessage) Fingerprint() any  { return nil }
func (m *EmptyRangeMessage) Count() int        { return 0 }
func (m *EmptyRangeMessage) Keys() []Ordered   { return nil }

// FingerprintMessage contains range fingerprint for comparison against the
// peer's fingerprint of the range with the same bounds [RangeX, RangeY)
type FingerprintMessage struct {
	RangeX, RangeY   CompactHash32
	RangeFingerprint types.Hash12
	NumItems         uint32
}

var _ SyncMessage = &FingerprintMessage{}

func (m *FingerprintMessage) Type() MessageType { return MessageTypeFingerprint }
func (m *FingerprintMessage) X() Ordered        { return m.RangeX.ToOrdered() }
func (m *FingerprintMessage) Y() Ordered        { return m.RangeY.ToOrdered() }
func (m *FingerprintMessage) Fingerprint() any  { return m.RangeFingerprint }
func (m *FingerprintMessage) Count() int        { return int(m.NumItems) }
func (m *FingerprintMessage) Keys() []Ordered   { return nil }

// RangeContentsMessage denotes a range for which the set of items has been sent.
// The peer needs to send back any items it has in the same range bounded
// by [RangeX, RangeY)
type RangeContentsMessage struct {
	RangeX, RangeY CompactHash32
	NumItems       uint32
}

var _ SyncMessage = &RangeContentsMessage{}

func (m *RangeContentsMessage) Type() MessageType { return MessageTypeRangeContents }
func (m *RangeContentsMessage) X() Ordered        { return m.RangeX.ToOrdered() }
func (m *RangeContentsMessage) Y() Ordered        { return m.RangeY.ToOrdered() }
func (m *RangeContentsMessage) Fingerprint() any  { return nil }
func (m *RangeContentsMessage) Count() int        { return int(m.NumItems) }
func (m *RangeContentsMessage) Keys() []Ordered   { return nil }

// ItemBatchMessage denotes a batch of items to be added to the peer's set.
type ItemBatchMessage struct {
	ContentKeys []types.Hash32 `scale:"max=1024"`
}

func (m *ItemBatchMessage) Type() MessageType { return MessageTypeItemBatch }
func (m *ItemBatchMessage) X() Ordered        { return nil }
func (m *ItemBatchMessage) Y() Ordered        { return nil }
func (m *ItemBatchMessage) Fingerprint() any  { return nil }
func (m *ItemBatchMessage) Count() int        { return 0 }
func (m *ItemBatchMessage) Keys() []Ordered {
	var r []Ordered
	for _, k := range m.ContentKeys {
		r = append(r, k)
	}
	return r
}

// ProbeMessage requests bounded range fingerprint and count from the peer,
// along with a minhash sample if fingerprints differ
type ProbeMessage struct {
	RangeX, RangeY   CompactHash32
	RangeFingerprint types.Hash12
	SampleSize       uint32
}

var _ SyncMessage = &ProbeMessage{}

func (m *ProbeMessage) Type() MessageType { return MessageTypeProbe }
func (m *ProbeMessage) X() Ordered        { return m.RangeX.ToOrdered() }
func (m *ProbeMessage) Y() Ordered        { return m.RangeY.ToOrdered() }
func (m *ProbeMessage) Fingerprint() any  { return m.RangeFingerprint }
func (m *ProbeMessage) Count() int        { return int(m.SampleSize) }
func (m *ProbeMessage) Keys() []Ordered   { return nil }

// MinhashSampleItem represents an item of minhash sample subset
type MinhashSampleItem uint32

var _ Ordered = MinhashSampleItem(0)

func (m MinhashSampleItem) String() string {
	return fmt.Sprintf("0x%08x", uint32(m))
}

// Compare implements Ordered
func (m MinhashSampleItem) Compare(other any) int {
	return cmp.Compare(m, other.(MinhashSampleItem))
}

// EncodeScale implements scale.Encodable.
func (m MinhashSampleItem) EncodeScale(e *scale.Encoder) (int, error) {
	// QQQQQ: FIXME: there's EncodeUint32 (non-compact which is better for hashes)
	// but no DecodeUint32
	return scale.EncodeCompact32(e, uint32(m))
}

// DecodeScale implements scale.Decodable.
func (m *MinhashSampleItem) DecodeScale(d *scale.Decoder) (int, error) {
	v, total, err := scale.DecodeCompact32(d)
	*m = MinhashSampleItem(v)
	return total, err
}

// MinhashSampleItemFromHash32 uses lower 32 bits of a Hash32 as a MinhashSampleItem
func MinhashSampleItemFromHash32(h types.Hash32) MinhashSampleItem {
	return MinhashSampleItem(uint32(h[28])<<24 + uint32(h[29])<<16 + uint32(h[30])<<8 + uint32(h[31]))
}

// SampleMessage is a sample of set items
type SampleMessage struct {
	RangeX, RangeY   CompactHash32
	RangeFingerprint types.Hash12
	NumItems         uint32
	// NOTE: max must be in sync with maxSampleSize in hashsync/rangesync.go
	Sample []MinhashSampleItem `scale:"max=1000"`
}

var _ SyncMessage = &SampleMessage{}

func (m *SampleMessage) Type() MessageType { return MessageTypeSample }
func (m *SampleMessage) X() Ordered        { return m.RangeX.ToOrdered() }
func (m *SampleMessage) Y() Ordered        { return m.RangeY.ToOrdered() }
func (m *SampleMessage) Fingerprint() any  { return m.RangeFingerprint }
func (m *SampleMessage) Count() int        { return int(m.NumItems) }

func (m *SampleMessage) Keys() []Ordered {
	r := make([]Ordered, len(m.Sample))
	for n, item := range m.Sample {
		r[n] = item
	}
	return r
}

// TODO: don't do scalegen for empty types
