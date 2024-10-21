package rangesync

import (
	"iter"

	"go.uber.org/zap/zapcore"
)

// Seq represents an ordered sequence of elements.
// Unless the sequence is empty or an error occurs while iterating, it yields elements
// endlessly, wrapping around to the first element after the last one.
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
	var (
		first KeyBytes
		r     []KeyBytes
	)
	for v := range s {
		if first == nil {
			first = v
		} else if v.Compare(first) == 0 {
			break
		}
		r = append(r, v)
	}
	return r
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

// EmptySeqResult returns an empty sequence result.
func EmptySeqResult() SeqResult {
	return SeqResult{
		Seq:   EmptySeq(),
		Error: func() error { return nil },
	}
}

// ErrorSeqResult returns a sequence result with an empty sequence and an error.
func ErrorSeqResult(err error) SeqResult {
	return SeqResult{
		Seq:   EmptySeq(),
		Error: SeqError(err),
	}
}
