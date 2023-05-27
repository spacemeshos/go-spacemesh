package tortoise

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

const (
	traceStart = 1 + iota
	traceWeakCoin
	traceBeacon
	traceDecode
	traceStore
	traceEncode
	traceTally
	traceHare
	traceActiveset
	traceResults
	traceUpdates
)

type ConfigTrace struct {
	Hdist                    uint32
	Zdist                    uint32
	WindowSize               uint32
	MaxExceptions            int
	BadBeaconVoteDelayLayers uint32
	LayerSize                uint32
	EpochSize                uint32 // this field is not set in the original config
}

type WeakCoinTrace struct {
	Layer types.LayerID
	Coin  bool
}

type BeaconTrace struct {
	Epoch  types.EpochID
	Beacon types.Beacon
}

type DecodeBallotTrace struct {
	Ballot types.Ballot
	Error  string

	//TODO(dshulyak) want to assert decoding results somehow
}

type EncodeVotesTrace struct {
	Layer   types.LayerID
	Opinion types.Opinion
	Error   string
}

type TallyVotesTrace struct {
	Layer types.LayerID
}

type HareTrace struct {
	Layer types.LayerID
	Vote  types.BlockID
}

type ResultsTrace struct {
	From, To types.LayerID
	Error    string
	Results  []result.Layer
}

type MissingActiveSetTrace struct {
	Epoch             types.EpochID
	Request, Response []types.ATXID
}

type Event struct {
	Timestamp time.Time
	Domain    int
	Object    any
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
	if t.backend == nil {
		return nil
	}
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

func (t *Tracer) onEvent(event *Event) {
	err := t.backend.OnEvent(event)
	if err == nil {
		return
	}
	select {
	case t.errors <- err:
	default:
	}
}

func (t *Tracer) OnStart(cfg *ConfigTrace) {
	t.onEvent(&Event{
		Timestamp: time.Now(),
		Domain:    1,
		Object:    cfg,
	})
}

// func (t *Tracer) OnWeakCoin()

func Open(path string) (*JsonWriter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("file at %s: %w", path, err)
	}
	buf := bufio.NewWriterSize(f, 1<<20)
	return &JsonWriter{
		f:   f,
		buf: buf,
		enc: json.NewEncoder(buf),
	}, nil
}

type JsonWriter struct {
	f   *os.File
	buf *bufio.Writer
	enc *json.Encoder
}

func (f *JsonWriter) OnEvent(event *Event) error {
	return f.enc.Encode(event)
}

func (f *JsonWriter) Close() error {
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
