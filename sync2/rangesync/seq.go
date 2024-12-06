package rangesync

import (
	"iter"
	"slices"

	"go.uber.org/zap/zapcore"
)

// Seq represents an ordered sequence of elements.
// Most sequences are finite. Infinite sequences are explicitly mentioned in the
// documentation of functions/methods that return them.
type Seq iter.Seq[KeyBytes]

var _ zapcore.ArrayMarshaler = Seq(nil)

// First returns the first element from the sequence, if any.
// If the sequence is empty, it returns nil.
func (s Seq) First() KeyBytes {
	for k := range s {
		return k
	}
	return nil
}

// FirstN returns the first n elements from the sequence.
func (s Seq) FirstN(n int) []KeyBytes {
	res := make([]KeyBytes, 0, n)
	for k := range s {
		if len(res) == n {
			break
		}
		res = append(res, k)
	}
	return res
}

// Collect returns all elements in the sequence as a slice.
// It may not be very efficient due to reallocations, and thus it should only be used for
// small sequences or for testing.
func (s Seq) Collect() []KeyBytes {
	return slices.Collect(iter.Seq[KeyBytes](s))
}

// MarshalLogArray implements zapcore.ArrayMarshaler.
func (s Seq) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	if s == nil {
		return nil
	}
	n := 0
	for k := range s {
		if n == 3 {
			enc.AppendString("...")
			break
		}
		enc.AppendString(k.ShortString())
		n++
	}
	return nil
}

// Limit limits sequence to n elements.
func (s Seq) Limit(n int) Seq {
	return Seq(func(yield func(KeyBytes) bool) {
		n := n // ensure reusability
		for k := range s {
			if n == 0 || !yield(k) {
				return
			}
			n--
		}
	})
}

// EmptySeq returns an empty sequence.
func EmptySeq() Seq {
	return Seq(func(yield func(KeyBytes) bool) {})
}

// SeqErrorFunc is a function that returns an error that happened during iteration, if
// any.
type SeqErrorFunc func() error

// NoSeqError is a SeqErrorFunc that always returns nil (no error).
var NoSeqError SeqErrorFunc = func() error { return nil }

// SeqError returns a SeqErrorFunc that always returns the given error.
func SeqError(err error) SeqErrorFunc {
	return func() error { return err }
}

// SeqResult represents the result of a function that returns a sequence.
// Error method most be called to check if an error occurred after
// processing the sequence.
// Error is reset at the beginning of each Seq call (iteration over the sequence).
type SeqResult struct {
	Seq   Seq
	Error SeqErrorFunc
}

// MarshalLogArray implements zapcore.ArrayMarshaler.
func (s SeqResult) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	s.Seq.MarshalLogArray(enc) // never returns an error
	return s.Error()
}

// First returns the first element from the result's sequence, if any.
// If the sequence is empty, it returns nil.
func (s SeqResult) First() (KeyBytes, error) {
	var r KeyBytes
	for r = range s.Seq {
		break
	}
	return r, s.Error()
}

// FirstN returns the first n elements from the result's sequence.
func (s SeqResult) FirstN(n int) ([]KeyBytes, error) {
	items := s.Seq.FirstN(n)
	return items, s.Error()
}

// Collect returns all elements in the result's sequence as a slice.
// It may not be very efficient due to reallocations, and thus it should only be used for
// small sequences or for testing.
func (s SeqResult) Collect() ([]KeyBytes, error) {
	return s.Seq.Collect(), s.Error()
}

// IsEmpty returns true if the sequence in SeqResult is empty.
// It also checks for errors.
func (s SeqResult) IsEmpty() (bool, error) {
	for range s.Seq {
		return false, s.Error()
	}
	return true, s.Error()
}

// Limit limits SeqResult to n elements.
func (s SeqResult) Limit(n int) SeqResult {
	return SeqResult{
		Seq:   s.Seq.Limit(n),
		Error: s.Error,
	}
}

// EmptySeqResult returns an empty sequence result.
func EmptySeqResult() SeqResult {
	return SeqResult{
		Seq:   EmptySeq(),
		Error: NoSeqError,
	}
}

// ErrorSeqResult returns a sequence result with an empty sequence and an error.
func ErrorSeqResult(err error) SeqResult {
	return SeqResult{
		Seq:   EmptySeq(),
		Error: SeqError(err),
	}
}

// MakeSeqResult makes a SeqResult out of a slice.
func MakeSeqResult(items []KeyBytes) SeqResult {
	return SeqResult{
		Seq: func(yield func(k KeyBytes) bool) {
			for _, item := range items {
				if !yield(item) {
					return
				}
			}
		},
		Error: NoSeqError,
	}
}
