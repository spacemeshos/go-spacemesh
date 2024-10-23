package fptree

import (
	"fmt"
	"os"
	"strings"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// trace represents a logging facility for tracing FPTree operations, using indentation to
// show their nested structure.
type trace struct {
	traceEnabled bool
	traceStack   []string
}

func (t *trace) out(msg string) {
	fmt.Fprintf(os.Stderr, "TRACE: %s%s\n", strings.Repeat("  ", len(t.traceStack)), msg)
}

// enter marks the entry to a function, printing the log message with the given format
// string and arguments.
func (t *trace) enter(format string, args ...any) {
	if !t.traceEnabled {
		return
	}
	for n, arg := range args {
		if sr, ok := arg.(rangesync.SeqResult); ok {
			args[n] = formatSeqResult(sr)
		}
	}
	msg := fmt.Sprintf(format, args...)
	t.out("ENTER: " + msg)
	t.traceStack = append(t.traceStack, msg)
}

// leave marks the exit from a function, printing the results of the function call
// together with the same log message contents which was used in the corresponding enter
// call.
func (t *trace) leave(results ...any) {
	if !t.traceEnabled {
		return
	}
	if len(t.traceStack) == 0 {
		panic("BUG: trace stack underflow")
	}
	for n, r := range results {
		if err, ok := r.(error); ok {
			results = []any{fmt.Sprintf("<error: %v>", err)}
			break
		}
		if sr, ok := r.(rangesync.SeqResult); ok {
			results[n] = formatSeqResult(sr)
		}
	}
	msg := t.traceStack[len(t.traceStack)-1]
	if len(results) != 0 {
		var r []string
		for _, res := range results {
			r = append(r, fmt.Sprint(res))
		}
		msg += " => " + strings.Join(r, ", ")
	}
	t.traceStack = t.traceStack[:len(t.traceStack)-1]
	t.out("LEAVE: " + msg)
}

// log prints a log message with the given format string and arguments.
func (t *trace) log(format string, args ...any) {
	if t.traceEnabled {
		for n, arg := range args {
			if sr, ok := arg.(rangesync.SeqResult); ok {
				args[n] = formatSeqResult(sr)
			}
		}
		msg := fmt.Sprintf(format, args...)
		t.out(msg)
	}
}

// seqFormatter is a lazy formatter for SeqResult.
type seqFormatter struct {
	sr rangesync.SeqResult
}

// String implements fmt.Stringer.
func (f seqFormatter) String() string {
	for k := range f.sr.Seq {
		return k.String()
	}
	if err := f.sr.Error(); err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return "<empty>"
}

// formatSeqResult returns a fmt.Stringer for the SeqResult that
// formats the sequence result lazily.
func formatSeqResult(sr rangesync.SeqResult) fmt.Stringer {
	return seqFormatter{sr: sr}
}
