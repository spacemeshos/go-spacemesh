package rangesync

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

// MessageType specifies the type of a sync message.
type MessageType byte

const (
	// Done message is sent to indicate the completion of the whole sync run.
	MessageTypeDone MessageType = iota
	// EndRoundMessage is sent to indicate the completion of a single round.
	MessageTypeEndRound
	// EmptySetMessage is sent to indicate that the set is empty. In response, the
	// receiving side sends its whole set using ItemBatch and RangeContents messages.
	MessageTypeEmptySet
	// EmptyRangeMessage is sent to indicate that the specified range is empty. In
	// response, the receiving side sends the contents of the range using ItemBatch
	// and RangeContents messages.
	MessageTypeEmptyRange
	// Fingerprint carries a range fingerprint. Depending on the local fingerprint of
	// the same range, the receiving side may not reply to this message (range
	// completed), may send the contents of the range using ItemBatch and
	// RangeContents messages if the range is small enough, or it can split the range
	// in two and send back the Fingerprint messages for each of resulting parts.
	MessageTypeFingerprint
	// RangeContents message is sent after ItemBatch messages to indicate what range
	// the messages belong too. If the receiving side has any items within the same
	// range, these items are sent back using ItemBatch and RangeContents messages.
	MessageTypeRangeContents
	// ItemBatchMessage is sent to carry a batch of items.
	MessageTypeItemBatch
	// ProbeMessage is sent to request the count of items and an estimate of the
	// Jaccard similarity coefficient for the specfied range.
	MessageTypeProbe
	// Sample message carries a minhash sample along with the fingerprint and
	// the fingerprint and number of items within the specied range.
	MessageTypeSample
	// Recent message is sent after ItemBatch messages to indicate that the batches
	// contains items recently added to the set.
	MessageTypeRecent
)

var messageTypes = []string{
	"done",
	"endRound",
	"emptySet",
	"emptyRange",
	"fingerprint",
	"rangeContents",
	"itemBatch",
	"probe",
	"sample",
	"recent",
}

// String implements Stringer.
func (mtype MessageType) String() string {
	if int(mtype) < len(messageTypes) {
		return messageTypes[mtype]
	}
	return fmt.Sprintf("<unknown %02x>", int(mtype))
}

// SyncMessageToString returns string representation of a sync message.
func SyncMessageToString(m SyncMessage) string {
	var sb strings.Builder
	sb.WriteString("<" + m.Type().String())
	if x := m.X(); x != nil {
		sb.WriteString(" X=" + x.String())
	}
	if y := m.Y(); y != nil {
		sb.WriteString(" Y=" + y.String())
	}
	if count := m.Count(); count != 0 {
		fmt.Fprintf(&sb, " Count=%d", count)
	}
	if fp := m.Fingerprint(); fp != types.EmptyFingerprint() {
		sb.WriteString(" FP=" + fp.String())
	}
	for _, k := range m.Keys() {
		fmt.Fprintf(&sb, " item=%s", k.String())
	}
	sb.WriteString(">")
	return sb.String()
}

type sender struct {
	Conduit
}

func (s sender) sendFingerprint(x, y types.KeyBytes, fp types.Fingerprint, count int) error {
	return s.Send(&FingerprintMessage{
		RangeX:           chash(x),
		RangeY:           chash(y),
		RangeFingerprint: fp,
		NumItems:         uint32(count),
	})
}

func (s sender) sendEmptySet() error {
	return s.Send(&EmptySetMessage{})
}

func (s sender) sendEmptyRange(x, y types.KeyBytes) error {
	return s.Send(&EmptyRangeMessage{
		RangeX: chash(x),
		RangeY: chash(y),
	})
}

func (s sender) sendRangeContents(x, y types.KeyBytes, count int) error {
	return s.Send(&RangeContentsMessage{
		RangeX:   chash(x),
		RangeY:   chash(y),
		NumItems: uint32(count),
	})
}

func (s sender) sendChunk(items []types.KeyBytes) error {
	msg := ItemBatchMessage{
		ContentKeys: KeyCollection{
			Keys: slices.Clone(items),
		},
	}
	return s.Send(&msg)
}

func (s sender) sendEndRound() error {
	return s.Send(&EndRoundMessage{})
}

func (s sender) sendDone() error {
	return s.Send(&DoneMessage{})
}

func (s sender) sendProbe(x, y types.KeyBytes, fp types.Fingerprint, sampleSize int) error {
	return s.Send(&ProbeMessage{
		RangeFingerprint: fp,
		SampleSize:       uint32(sampleSize),
		RangeX:           chash(x),
		RangeY:           chash(y),
	})
}

func (s sender) sendSample(
	x, y types.KeyBytes,
	fp types.Fingerprint,
	count, sampleSize int,
	seq types.Seq,
) error {
	items, err := Sample(seq, count, sampleSize)
	if err != nil {
		return err
	}
	return s.Send(&SampleMessage{
		RangeFingerprint: fp,
		NumItems:         uint32(count),
		SampleItems:      items,
		RangeX:           chash(x),
		RangeY:           chash(y),
	})
}

func (s sender) sendRecent(since time.Time) error {
	return s.Send(&RecentMessage{
		SinceTime: uint64(since.UnixNano()),
	})
}

// "Empty" message types follow. These do not need scalegen and thus are not in wire_types.go.

type Marker struct{}

func (*Marker) X() types.KeyBytes              { return nil }
func (*Marker) Y() types.KeyBytes              { return nil }
func (*Marker) Fingerprint() types.Fingerprint { return types.EmptyFingerprint() }
func (*Marker) Count() int                     { return 0 }
func (*Marker) Keys() []types.KeyBytes         { return nil }
func (*Marker) Since() time.Time               { return time.Time{} }
func (*Marker) Sample() []MinhashSampleItem    { return nil }

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
// peer to send all of its items.
type EmptySetMessage struct{ Marker }

var _ SyncMessage = &EmptySetMessage{}

func (*EmptySetMessage) Type() MessageType { return MessageTypeEmptySet }
