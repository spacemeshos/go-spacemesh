package hashsync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

type Marker struct{}

func (*Marker) X() Ordered       { return nil }
func (*Marker) Y() Ordered       { return nil }
func (*Marker) Fingerprint() any { return nil }
func (*Marker) Count() int       { return 0 }
func (*Marker) Keys() []Ordered  { return nil }
func (*Marker) Values() []any    { return nil }

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
	RangeX, RangeY types.Hash32
}

var _ SyncMessage = &EmptyRangeMessage{}

func (m *EmptyRangeMessage) Type() MessageType { return MessageTypeEmptyRange }
func (m *EmptyRangeMessage) X() Ordered        { return m.RangeX }
func (m *EmptyRangeMessage) Y() Ordered        { return m.RangeY }
func (m *EmptyRangeMessage) Fingerprint() any  { return nil }
func (m *EmptyRangeMessage) Count() int        { return 0 }
func (m *EmptyRangeMessage) Keys() []Ordered   { return nil }
func (m *EmptyRangeMessage) Values() []any     { return nil }

// FingerprintMessage contains range fingerprint for comparison against the
// peer's fingerprint of the range with the same bounds [RangeX, RangeY)
type FingerprintMessage struct {
	RangeX, RangeY   types.Hash32
	RangeFingerprint types.Hash12
	NumItems         uint32
}

var _ SyncMessage = &FingerprintMessage{}

func (m *FingerprintMessage) Type() MessageType { return MessageTypeFingerprint }
func (m *FingerprintMessage) X() Ordered        { return m.RangeX }
func (m *FingerprintMessage) Y() Ordered        { return m.RangeY }
func (m *FingerprintMessage) Fingerprint() any  { return m.RangeFingerprint }
func (m *FingerprintMessage) Count() int        { return int(m.NumItems) }
func (m *FingerprintMessage) Keys() []Ordered   { return nil }
func (m *FingerprintMessage) Values() []any     { return nil }

// RangeContentsMessage denotes a range for which the set of items has been sent.
// The peer needs to send back any items it has in the same range bounded
// by [RangeX, RangeY)
type RangeContentsMessage struct {
	RangeX, RangeY types.Hash32
	NumItems       uint32
}

var _ SyncMessage = &RangeContentsMessage{}

func (m *RangeContentsMessage) Type() MessageType { return MessageTypeRangeContents }
func (m *RangeContentsMessage) X() Ordered        { return m.RangeX }
func (m *RangeContentsMessage) Y() Ordered        { return m.RangeY }
func (m *RangeContentsMessage) Fingerprint() any  { return nil }
func (m *RangeContentsMessage) Count() int        { return int(m.NumItems) }
func (m *RangeContentsMessage) Keys() []Ordered   { return nil }
func (m *RangeContentsMessage) Values() []any     { return nil }

// ItemBatchMessage denotes a batch of items to be added to the peer's set.
// ItemBatchMessage doesn't implement SyncMessage interface by itself
// and needs to be wrapped in TypedItemBatchMessage[T] that implements
// SyncMessage by providing the proper Values() method
type ItemBatchMessage struct {
	ContentKeys   []types.Hash32 `scale:"max=1024"`
	ContentValues []byte         `scale:"max=1024"`
}

func (m *ItemBatchMessage) Type() MessageType { return MessageTypeItemBatch }

// TODO: don't do scalegen for empty types
