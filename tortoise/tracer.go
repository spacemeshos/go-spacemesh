package tortoise

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type Event struct {
	Type  eventType
	Event scale.Encodable
}

func (e *Event) EncodeScale(enc *scale.Encoder) (int, error) {
	total := 0
	{
		n, err := scale.EncodeCompact16(enc, e.Type)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := e.Event.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

type backend interface {
	OnEvent(*Event) error
	Close() error
}

type Tracer struct {
	errors  chan error
	backend backend
}

func (t *Tracer) Close() error {
	return t.backend.Close()
}

func (t *Tracer) Wait(ctx context.Context) error {
	select {
	case err := <-t.errors:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Tracer) On(event traceEvent) {
	err := t.backend.OnEvent(&Event{
		Type:  event.Type(),
		Event: event,
	})
	if err == nil {
		return
	}
	select {
	case t.errors <- err:
	default:
	}
}

func NewTracer(path string) (*Tracer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("file at %s: %w", path, err)
	}
	buf := bufio.NewWriterSize(f, 1<<20)
	codec := &writer{
		f:   f,
		buf: buf,
		enc: scale.NewEncoder(buf),
	}
	return &Tracer{
		errors:  make(chan error, 1),
		backend: codec,
	}, nil
}

type writer struct {
	f   *os.File
	buf *bufio.Writer
	enc *scale.Encoder
}

func (f *writer) OnEvent(event *Event) error {
	_, err := event.EncodeScale(f.enc)
	return err
}

func (f *writer) Close() error {
	err1 := f.buf.Flush()
	err2 := f.f.Sync()
	err3 := f.f.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}

type traceRunner struct {
	opts          []Opt
	trt           *Tortoise
	pending       map[types.BallotID]*DecodedBallot
	assertOutputs bool
	assertErrors  bool
}

func RunTrace(path string, opts ...Opt) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	dec := scale.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	enum := NewEventEnum()
	runner := &traceRunner{
		opts:          opts,
		pending:       map[types.BallotID]*DecodedBallot{},
		assertOutputs: true,
		assertErrors:  true,
	}
	for {
		ev, err := enum.Decode(dec)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := ev.Run(runner); err != nil {
			return err
		}
	}
}
