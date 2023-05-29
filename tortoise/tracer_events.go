package tortoise

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

type eventType = uint16

const (
	traceStart eventType = 1 + iota
	traceWeakCoin
	traceBeacon
	traceAtx
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
	scale.Type
	Type() eventType
	New() traceEvent
	Run(*traceRunner) error
}

//go:generate scalegen

type ConfigTrace struct {
	Hdist                    uint32
	Zdist                    uint32
	WindowSize               uint32
	MaxExceptions            uint32
	BadBeaconVoteDelayLayers uint32
	LayerSize                uint32
	EpochSize                uint32 // this field is not set in the original config
}

func (c *ConfigTrace) Type() eventType {
	return traceStart
}

func (c *ConfigTrace) New() traceEvent {
	return &ConfigTrace{}
}

func (c *ConfigTrace) Run(r *traceRunner) error {
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
	types.SetLayersPerEpoch(c.EpochSize)
	return nil
}

type AtxTrace struct {
	Header *types.ActivationTxHeader
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
	Layer types.LayerID
	Coin  bool
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
	Epoch  types.EpochID
	Beacon types.Beacon
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

type DecodeBallotTrace struct {
	Ballot *types.Ballot
	Error  string `scale:"max=100000"`

	//TODO(dshulyak) want to assert decoding results somehow
}

func (d *DecodeBallotTrace) Type() eventType {
	return traceDecode
}

func (d *DecodeBallotTrace) New() traceEvent {
	return &DecodeBallotTrace{}
}

func (b *DecodeBallotTrace) Run(r *traceRunner) error {
	if err := b.Ballot.Initialize(); err != nil {
		return err
	}
	decoded, err := r.trt.DecodeBallot(b.Ballot)
	if err == nil {
		r.pending[decoded.ID()] = decoded
	}
	return nil
}

type StoreBallotTrace struct {
	ID        types.BallotID
	Malicious bool
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
	Layer   types.LayerID
	Opinion *types.Opinion
	Error   string `scale:"max=100000"`
}

func (e *EncodeVotesTrace) Type() eventType {
	return traceEncode
}

func (e *EncodeVotesTrace) New() traceEvent {
	return &EncodeVotesTrace{}
}

func (e *EncodeVotesTrace) Run(r *traceRunner) error {
	opinion, err := r.trt.EncodeVotes(context.Background(), EncodeVotesWithCurrent(e.Layer))
	if err == nil {
		if diff := cmp.Diff(opinion, e.Opinion); len(diff) > 0 && r.assertOutputs {
			return errors.New(diff)
		}
	}
	return nil
}

type TallyTrace struct {
	Layer types.LayerID
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
	Layer types.LayerID
	Vote  types.BlockID
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
	From, To types.LayerID
	Error    string         `scale:"max=100000"`
	Results  []result.Layer `scale:"max=100000"`
}

func (r *ResultsTrace) Type() eventType {
	return traceResults
}

func (r *ResultsTrace) New() traceEvent {
	return &ResultsTrace{}
}

func (r *ResultsTrace) Run(rt *traceRunner) error {
	rst, err := rt.trt.Results(r.From, r.To)
	if err == nil {
		if diff := cmp.Diff(rst, r.Results); len(diff) > 0 && rt.assertOutputs {
			return errors.New(diff)
		}
	}
	return nil
}

type UpdatesTrace struct {
	ResultsTrace
}

func (u *UpdatesTrace) Type() eventType {
	return traceUpdates
}

func (u *UpdatesTrace) New() traceEvent {
	return &UpdatesTrace{}
}

func (u *UpdatesTrace) Run(r *traceRunner) error {
	rst := r.trt.Updates()
	if diff := cmp.Diff(rst, u.Results); len(diff) > 0 && r.assertOutputs {
		return errors.New(diff)
	}
	return nil
}

type BlockTrace struct {
	Header types.BlockHeader
	Valid  bool
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
