package rangesync

import (
	"time"
)

//go:generate scalegen

// EmptyRangeMessage notifies the peer that it needs to send all of its items in
// the specified range.
type EmptyRangeMessage struct {
	RangeX, RangeY CompactHash
}

var _ SyncMessage = &EmptyRangeMessage{}

func (m *EmptyRangeMessage) Type() MessageType           { return MessageTypeEmptyRange }
func (m *EmptyRangeMessage) X() KeyBytes                 { return m.RangeX.ToOrdered() }
func (m *EmptyRangeMessage) Y() KeyBytes                 { return m.RangeY.ToOrdered() }
func (m *EmptyRangeMessage) Fingerprint() Fingerprint    { return EmptyFingerprint() }
func (m *EmptyRangeMessage) Count() int                  { return 0 }
func (m *EmptyRangeMessage) Keys() []KeyBytes            { return nil }
func (m *EmptyRangeMessage) Since() time.Time            { return time.Time{} }
func (m *EmptyRangeMessage) Sample() []MinhashSampleItem { return nil }

// FingerprintMessage contains range fingerprint for comparison against the
// peer's fingerprint of the range with the same bounds [RangeX, RangeY).
type FingerprintMessage struct {
	RangeX, RangeY   CompactHash
	RangeFingerprint Fingerprint
	NumItems         uint32
}

var _ SyncMessage = &FingerprintMessage{}

func (m *FingerprintMessage) Type() MessageType           { return MessageTypeFingerprint }
func (m *FingerprintMessage) X() KeyBytes                 { return m.RangeX.ToOrdered() }
func (m *FingerprintMessage) Y() KeyBytes                 { return m.RangeY.ToOrdered() }
func (m *FingerprintMessage) Fingerprint() Fingerprint    { return m.RangeFingerprint }
func (m *FingerprintMessage) Count() int                  { return int(m.NumItems) }
func (m *FingerprintMessage) Keys() []KeyBytes            { return nil }
func (m *FingerprintMessage) Since() time.Time            { return time.Time{} }
func (m *FingerprintMessage) Sample() []MinhashSampleItem { return nil }

// RangeContentsMessage denotes a range for which the set of items has been sent.
// The peer needs to send back any items it has in the same range bounded
// by [RangeX, RangeY).
type RangeContentsMessage struct {
	RangeX, RangeY CompactHash
	NumItems       uint32
}

var _ SyncMessage = &RangeContentsMessage{}

func (m *RangeContentsMessage) Type() MessageType           { return MessageTypeRangeContents }
func (m *RangeContentsMessage) X() KeyBytes                 { return m.RangeX.ToOrdered() }
func (m *RangeContentsMessage) Y() KeyBytes                 { return m.RangeY.ToOrdered() }
func (m *RangeContentsMessage) Fingerprint() Fingerprint    { return EmptyFingerprint() }
func (m *RangeContentsMessage) Count() int                  { return int(m.NumItems) }
func (m *RangeContentsMessage) Keys() []KeyBytes            { return nil }
func (m *RangeContentsMessage) Since() time.Time            { return time.Time{} }
func (m *RangeContentsMessage) Sample() []MinhashSampleItem { return nil }

// ItemBatchMessage denotes a batch of items to be added to the peer's set.
type ItemBatchMessage struct {
	ContentKeys KeyCollection `scale:"max=1024"`
}

var _ SyncMessage = &ItemBatchMessage{}

func (m *ItemBatchMessage) Type() MessageType        { return MessageTypeItemBatch }
func (m *ItemBatchMessage) X() KeyBytes              { return nil }
func (m *ItemBatchMessage) Y() KeyBytes              { return nil }
func (m *ItemBatchMessage) Fingerprint() Fingerprint { return EmptyFingerprint() }
func (m *ItemBatchMessage) Count() int               { return 0 }
func (m *ItemBatchMessage) Keys() []KeyBytes {
	return m.ContentKeys.Keys
}
func (m *ItemBatchMessage) Since() time.Time            { return time.Time{} }
func (m *ItemBatchMessage) Sample() []MinhashSampleItem { return nil }

// ProbeMessage requests bounded range fingerprint and count from the peer,
// along with a minhash sample if fingerprints differ.
type ProbeMessage struct {
	RangeX, RangeY   CompactHash
	RangeFingerprint Fingerprint
	SampleSize       uint32
}

var _ SyncMessage = &ProbeMessage{}

func (m *ProbeMessage) Type() MessageType           { return MessageTypeProbe }
func (m *ProbeMessage) X() KeyBytes                 { return m.RangeX.ToOrdered() }
func (m *ProbeMessage) Y() KeyBytes                 { return m.RangeY.ToOrdered() }
func (m *ProbeMessage) Fingerprint() Fingerprint    { return m.RangeFingerprint }
func (m *ProbeMessage) Count() int                  { return int(m.SampleSize) }
func (m *ProbeMessage) Keys() []KeyBytes            { return nil }
func (m *ProbeMessage) Since() time.Time            { return time.Time{} }
func (m *ProbeMessage) Sample() []MinhashSampleItem { return nil }

// SampleMessage is a sample of set items.
type SampleMessage struct {
	RangeX, RangeY   CompactHash
	RangeFingerprint Fingerprint
	NumItems         uint32
	// NOTE: max must be in sync with maxSampleSize in hashsync/rangesync.go
	SampleItems []MinhashSampleItem `scale:"max=1000"`
}

var _ SyncMessage = &SampleMessage{}

func (m *SampleMessage) Type() MessageType           { return MessageTypeSample }
func (m *SampleMessage) X() KeyBytes                 { return m.RangeX.ToOrdered() }
func (m *SampleMessage) Y() KeyBytes                 { return m.RangeY.ToOrdered() }
func (m *SampleMessage) Fingerprint() Fingerprint    { return m.RangeFingerprint }
func (m *SampleMessage) Count() int                  { return int(m.NumItems) }
func (m *SampleMessage) Keys() []KeyBytes            { return nil }
func (m *SampleMessage) Since() time.Time            { return time.Time{} }
func (m *SampleMessage) Sample() []MinhashSampleItem { return m.SampleItems }

// RecentMessage is a SyncMessage that denotes a set of items that have been
// added to the peer's set since the specific point in time.
type RecentMessage struct {
	SinceTime uint64
}

var _ SyncMessage = &RecentMessage{}

func (m *RecentMessage) Type() MessageType        { return MessageTypeRecent }
func (m *RecentMessage) X() KeyBytes              { return nil }
func (m *RecentMessage) Y() KeyBytes              { return nil }
func (m *RecentMessage) Fingerprint() Fingerprint { return EmptyFingerprint() }
func (m *RecentMessage) Count() int               { return 0 }
func (m *RecentMessage) Keys() []KeyBytes         { return nil }
func (m *RecentMessage) Since() time.Time {
	if m.SinceTime == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(m.SinceTime))
}
func (m *RecentMessage) Sample() []MinhashSampleItem { return nil }
