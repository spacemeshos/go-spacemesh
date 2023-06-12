package tortoise

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

type output struct {
	Type  eventType       `json:"t"`
	Event json.RawMessage `json:"o"`
}

type tracer struct {
	logger *zap.Logger
}

func (t *tracer) On(event traceEvent) {
	buf, err := json.Marshal(event)
	if err != nil {
		panic(err.Error())
	}
	raw := json.RawMessage(buf)
	t.logger.Info("",
		zap.Uint16("t", event.Type()),
		zap.Any("o", &raw),
	)
}

type TraceOpt func(*zap.Config)

func WithOutput(path string) TraceOpt {
	return func(cfg *zap.Config) {
		cfg.OutputPaths = []string{path}
	}
}

func newTracer(opts ...TraceOpt) *tracer {
	cfg := zap.NewProductionConfig()
	cfg.Sampling = nil
	cfg.EncoderConfig.CallerKey = zapcore.OmitKey
	cfg.EncoderConfig.MessageKey = zapcore.OmitKey
	cfg.EncoderConfig.LevelKey = zapcore.OmitKey
	cfg.DisableCaller = true
	for _, opt := range opts {
		opt(&cfg)
	}
	logger, err := cfg.Build()
	if err != nil {
		panic(err.Error())
	}
	return &tracer{
		logger: logger.Named("tracer"),
	}
}

type traceRunner struct {
	opts          []Opt
	trt           *Tortoise
	pending       map[types.BallotID]*DecodedBallot
	assertOutputs bool
	assertErrors  bool
}

func RunTrace(path string, breakpoint func(), opts ...Opt) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	enum := newEventEnum()
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
		if breakpoint != nil {
			breakpoint()
		}
	}
}

type eventType = uint16

const (
	traceStart eventType = 1 + iota
	traceWeakCoin
	traceBeacon
	traceAtx
	traceBallot
	traceDecode
	traceStore
	traceEncode
	traceTally
	traceBlock
	traceHare
	traceActiveset
	traceResults
	traceUpdates
)

type traceEvent interface {
	Type() eventType
	New() traceEvent
	Run(*traceRunner) error
}

type ConfigTrace struct {
	Hdist                    uint32 `json:"hdist"`
	Zdist                    uint32 `json:"zdist"`
	WindowSize               uint32 `json:"window"`
	MaxExceptions            uint32 `json:"exceptions"`
	BadBeaconVoteDelayLayers uint32 `json:"delay"`
	LayerSize                uint32 `json:"layer-size"`
	EpochSize                uint32 `json:"epoch-size"` // this field is not set in the original config
	EffectiveGenesis         uint32 `json:"effective-genesis"`
}

func (c *ConfigTrace) Type() eventType {
	return traceStart
}

func (c *ConfigTrace) New() traceEvent {
	return &ConfigTrace{}
}

func (c *ConfigTrace) Run(r *traceRunner) error {
	types.SetLayersPerEpoch(c.EpochSize)
	types.SetEffectiveGenesis(c.EffectiveGenesis)
	trt, err := New(append(r.opts, WithConfig(Config{
		Hdist:                    c.Hdist,
		Zdist:                    c.Zdist,
		WindowSize:               c.WindowSize,
		MaxExceptions:            int(c.MaxExceptions),
		BadBeaconVoteDelayLayers: c.BadBeaconVoteDelayLayers,
		LayerSize:                c.LayerSize,
	}))...)
	if err != nil {
		return err
	}
	r.trt = trt
	return nil
}

type AtxTrace struct {
	Header *types.AtxTortoiseData `json:",inline"`
}

func (a *AtxTrace) Type() eventType {
	return traceAtx
}

func (a *AtxTrace) New() traceEvent {
	return &AtxTrace{}
}

func (a *AtxTrace) Run(r *traceRunner) error {
	r.trt.OnAtx(a.Header)
	return nil
}

type WeakCoinTrace struct {
	Layer types.LayerID `json:"lid"`
	Coin  bool          `json:"coin"`
}

func (w *WeakCoinTrace) Type() eventType {
	return traceWeakCoin
}

func (w *WeakCoinTrace) New() traceEvent {
	return &WeakCoinTrace{}
}

func (w *WeakCoinTrace) Run(r *traceRunner) error {
	r.trt.OnWeakCoin(w.Layer, w.Coin)
	return nil
}

type BeaconTrace struct {
	Epoch  types.EpochID `json:"epoch"`
	Beacon types.Beacon  `json:"beacon"`
}

func (b *BeaconTrace) Type() eventType {
	return traceBeacon
}

func (b *BeaconTrace) New() traceEvent {
	return &BeaconTrace{}
}

func (b *BeaconTrace) Run(r *traceRunner) error {
	r.trt.OnBeacon(b.Epoch, b.Beacon)
	return nil
}

type BallotTrace struct {
	Ballot *types.BallotTortoiseData `json:",inline"`
}

func (b *BallotTrace) Type() eventType {
	return traceBallot
}

func (b *BallotTrace) New() traceEvent {
	return &BallotTrace{}
}

func (b *BallotTrace) Run(r *traceRunner) error {
	r.trt.OnBallot(b.Ballot)
	return nil
}

type DecodeBallotTrace struct {
	Ballot *types.BallotTortoiseData `json:",inline"`
	Error  string                    `json:"e"`

	// TODO(dshulyak) want to assert decoding results somehow
}

func (d *DecodeBallotTrace) Type() eventType {
	return traceDecode
}

func (d *DecodeBallotTrace) New() traceEvent {
	return &DecodeBallotTrace{}
}

func (b *DecodeBallotTrace) Run(r *traceRunner) error {
	decoded, err := r.trt.DecodeBallot(b.Ballot)
	if r.assertErrors {
		if err := assertErrors(err, b.Error); err != nil {
			return err
		}
	}
	if err == nil {
		r.pending[decoded.ID] = decoded
	}
	return nil
}

type StoreBallotTrace struct {
	ID        types.BallotID `json:"id"`
	Malicious bool           `json:"mal"`
}

func (s *StoreBallotTrace) Type() eventType {
	return traceStore
}

func (s *StoreBallotTrace) New() traceEvent {
	return &StoreBallotTrace{}
}

func (s *StoreBallotTrace) Run(r *traceRunner) error {
	pending, exist := r.pending[s.ID]
	if !exist {
		return fmt.Errorf("id %v should be pending", s.ID)
	}
	if s.Malicious {
		pending.SetMalicious()
	}
	delete(r.pending, s.ID)
	r.trt.StoreBallot(pending)
	return nil
}

type EncodeVotesTrace struct {
	Layer   types.LayerID  `json:"lid"`
	Opinion *types.Opinion `json:"opinion"`
	Error   string         `json:"e"`
}

func (e *EncodeVotesTrace) Type() eventType {
	return traceEncode
}

func (e *EncodeVotesTrace) New() traceEvent {
	return &EncodeVotesTrace{}
}

func (e *EncodeVotesTrace) Run(r *traceRunner) error {
	opinion, err := r.trt.EncodeVotes(context.Background(), EncodeVotesWithCurrent(e.Layer))
	if r.assertErrors {
		if err := assertErrors(err, e.Error); err != nil {
			return err
		}
	}
	if err == nil {
		if diff := cmp.Diff(opinion, e.Opinion); len(diff) > 0 && r.assertOutputs {
			return errors.New(diff)
		}
	}
	return nil
}

type TallyTrace struct {
	Layer types.LayerID `json:"lid"`
}

func (t *TallyTrace) Type() eventType {
	return traceTally
}

func (t *TallyTrace) New() traceEvent {
	return &TallyTrace{}
}

func (t *TallyTrace) Run(r *traceRunner) error {
	r.trt.TallyVotes(context.Background(), t.Layer)
	return nil
}

type HareTrace struct {
	Layer types.LayerID `json:"lid"`
	Vote  types.BlockID `json:"vote"`
}

func (h *HareTrace) Type() eventType {
	return traceHare
}

func (h *HareTrace) New() traceEvent {
	return &HareTrace{}
}

func (h *HareTrace) Run(r *traceRunner) error {
	r.trt.OnHareOutput(h.Layer, h.Vote)
	return nil
}

type ResultsTrace struct {
	From    types.LayerID  `json:"from"`
	To      types.LayerID  `json:"to"`
	Error   string         `json:"e"`
	Results []result.Layer `json:"results"`
}

func (r *ResultsTrace) Type() eventType {
	return traceResults
}

func (r *ResultsTrace) New() traceEvent {
	return &ResultsTrace{}
}

func (r *ResultsTrace) Run(rt *traceRunner) error {
	rst, err := rt.trt.Results(r.From, r.To)
	if rt.assertErrors {
		if err := assertErrors(err, r.Error); err != nil {
			return err
		}
	}
	if err == nil {
		if diff := cmp.Diff(rst, r.Results, cmpopts.EquateEmpty()); len(diff) > 0 && rt.assertOutputs {
			return errors.New(diff)
		}
	}
	return nil
}

type UpdatesTrace struct {
	ResultsTrace `json:",inline"`
}

func (u *UpdatesTrace) Type() eventType {
	return traceUpdates
}

func (u *UpdatesTrace) New() traceEvent {
	return &UpdatesTrace{}
}

func (u *UpdatesTrace) Run(r *traceRunner) error {
	rst := r.trt.Updates()
	if diff := cmp.Diff(rst, u.Results, cmpopts.EquateEmpty()); len(diff) > 0 && r.assertOutputs {
		return errors.New(diff)
	}
	return nil
}

type BlockTrace struct {
	Header types.BlockHeader `json:",inline"`
	Valid  bool              `json:"v"`
}

func (b *BlockTrace) Type() eventType {
	return traceBlock
}

func (b *BlockTrace) New() traceEvent {
	return &BlockTrace{}
}

func (b *BlockTrace) Run(r *traceRunner) error {
	if b.Valid {
		r.trt.OnValidBlock(b.Header)
	} else {
		r.trt.OnBlock(b.Header)
	}
	return nil
}

func assertErrors(err error, expect string) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if expect != msg {
		return fmt.Errorf("%s != %s", expect, msg)
	}
	return nil
}

func newEventEnum() eventEnum {
	enum := eventEnum{types: map[uint16]traceEvent{}}
	enum.Register(&ConfigTrace{})
	enum.Register(&WeakCoinTrace{})
	enum.Register(&BeaconTrace{})
	enum.Register(&AtxTrace{})
	enum.Register(&BallotTrace{})
	enum.Register(&DecodeBallotTrace{})
	enum.Register(&StoreBallotTrace{})
	enum.Register(&EncodeVotesTrace{})
	enum.Register(&TallyTrace{})
	enum.Register(&BlockTrace{})
	enum.Register(&HareTrace{})
	enum.Register(&ResultsTrace{})
	enum.Register(&UpdatesTrace{})
	return enum
}

type eventEnum struct {
	types map[eventType]traceEvent
}

func (e *eventEnum) Register(ev traceEvent) {
	e.types[ev.Type()] = ev
}

func (e *eventEnum) Decode(dec *json.Decoder) (traceEvent, error) {
	var event output
	if err := dec.Decode(&event); err != nil {
		return nil, err
	}
	ev := e.types[event.Type]
	if ev == nil {
		return nil, fmt.Errorf("type %d is not registered", event.Type)
	}
	obj := ev.New()
	if err := json.Unmarshal(event.Event, obj); err != nil {
		return nil, err
	}
	return obj, nil
}
