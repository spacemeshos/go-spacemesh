package tortoise

import (
	"context"
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/stretchr/testify/require"
)

type smesher struct {
	reg registry

	id       int
	defaults types.ActivationTxHeader
	atxs     map[uint32]*atxAction
}

type atxOpt func(*types.ActivationTxHeader)

type aopt struct {
	opts []atxOpt
}

func (a *aopt) base(val uint64) *aopt {
	a.opts = append(a.opts, func(header *types.ActivationTxHeader) {
		header.BaseTickHeight = val
	})
	return a
}

func (a *aopt) ticks(val uint64) *aopt {
	a.opts = append(a.opts, func(header *types.ActivationTxHeader) {
		header.TickCount = val
	})
	return a
}

func (a *aopt) units(val uint32) *aopt {
	a.opts = append(a.opts, func(header *types.ActivationTxHeader) {
		header.EffectiveNumUnits = val
	})
	return a
}

// atx accepts publish epoch and atx fields that are relevant for tortoise
// as options
func (s *smesher) atx(epoch uint32, opts ...*aopt) *atxAction {
	if val, exists := s.atxs[epoch]; exists {
		return val
	}
	header := s.defaults
	header.ID = hash.Sum([]byte(strconv.Itoa(s.id)), []byte(strconv.Itoa(int(epoch))))
	header.PublishEpoch = types.EpochID(epoch)
	for _, opt := range opts {
		for _, o := range opt.opts {
			o(&header)
		}
	}
	val := &atxAction{
		reg:     s.reg,
		header:  header,
		ballots: map[uint32]*ballotAction{},
	}
	s.reg.register(val)
	s.atxs[epoch] = val
	return val
}

type atxAction struct {
	reg registry

	header    types.ActivationTxHeader
	ballots   map[uint32]*ballotAction
	reference *ballotAction
}

type ballotOpt func(*types.Ballot)

type bopt struct {
	opts []ballotOpt
}

func (b *bopt) beacon(value string) *bopt {
	b.opts = append(b.opts, func(ballot *types.Ballot) {
		if ballot.EpochData == nil {
			ballot.EpochData = &types.EpochData{}
		}
		copy(ballot.EpochData.Beacon[:], value)
	})
	return b
}

func (b *bopt) eligibilities(value int) *bopt {
	b.opts = append(b.opts, func(ballot *types.Ballot) {
		ballot.EligibilityProofs = make([]types.VotingEligibility, value)
	})
	return b
}

func (b *bopt) activeset(values ...*atxAction) *bopt {
	b.opts = append(b.opts, func(ballot *types.Ballot) {
		for _, val := range values {
			ballot.ActiveSet = append(ballot.ActiveSet, val.header.ID)
		}
	})
	return b
}

func (b *bopt) votes(value types.Votes) *bopt {
	b.opts = append(b.opts, func(ballot *types.Ballot) {
		ballot.Votes = value
	})
	return b
}

func (b *bopt) malicious() *bopt {
	b.opts = append(b.opts, func(ballot *types.Ballot) {
		ballot.SetMalicious()
	})
	return b
}

func (a *atxAction) execute(trt *Tortoise) {
	trt.OnAtx(&a.header)
}

func (a *atxAction) ballot(lid uint32, opts ...*bopt) *ballotAction {
	lid = lid + types.GetEffectiveGenesis().Uint32()
	if val, exist := a.ballots[lid]; exist {
		return val
	}
	b := types.Ballot{}
	b.AtxID = a.header.ID
	b.Layer = types.LayerID(lid)
	hs := hash.Sum(a.header.ID[:], []byte(strconv.Itoa(int(lid))))
	id := types.BallotID{}
	copy(id[:], hs[:])
	b.SetID(id)
	for _, opt := range opts {
		for _, o := range opt.opts {
			o(&b)
		}
	}
	val := &ballotAction{ballot: b}
	if a.reference == nil {
		a.reference = val
	} else {
		val.ballot.RefBallot = a.reference.ballot.ID()
	}
	a.reg.register(val)
	a.ballots[lid] = val
	return val
}

type ballotAction struct {
	ballot types.Ballot
}

func (b *ballotAction) execute(trt *Tortoise) {
	trt.OnBallot(&b.ballot)
}

type action interface {
	execute(*Tortoise)
}

type session struct {
	config *Config

	smeshers map[int]*smesher
	actions  []action
}

type registry interface {
	register(...action)
}

func (s *session) smesher(id int) *smesher {
	if s.smeshers == nil {
		s.smeshers = map[int]*smesher{}
	}
	if val, exist := s.smeshers[id]; exist {
		return val
	}
	val := &smesher{reg: s, id: id, atxs: map[uint32]*atxAction{}}
	s.smeshers[id] = val
	return val
}

func (s *session) register(actions ...action) {
	s.actions = append(s.actions, actions...)
}

type tallyAction struct {
	lid uint32
}

func (t *tallyAction) execute(trt *Tortoise) {
	trt.TallyVotes(context.Background(), types.LayerID(t.lid))
}

func (s *session) tally(lid uint32) {
	s.register(&tallyAction{uint32(types.GetEffectiveGenesis()) + lid})
}

type hareAction struct {
	lid   types.LayerID
	block string
}

func (h *hareAction) execute(trt *Tortoise) {
	id := types.BlockID{}
	copy(id[:], h.block)
	trt.OnHareOutput(h.lid, id)
}

type blockAction struct {
	header types.BlockHeader
}

func (b *blockAction) execute(trt *Tortoise) {
	trt.OnBlock(b.header)
}

func (s *session) hare(lid uint32, id string) {
	s.register(&hareAction{lid: types.GetEffectiveGenesis() + types.LayerID(lid), block: id})
}

func (s *session) block(lid uint32, id string, height uint64) {
	header := types.BlockHeader{
		LayerID: types.GetEffectiveGenesis() + types.LayerID(lid),
		Height:  height,
	}
	copy(header.ID[:], id)
	s.register(&blockAction{header})
}

func (s *session) hareblock(lid uint32, id string, height uint64) {
	s.block(lid, id, height)
	s.hare(lid, id)
}

type beaconAction struct {
	epoch  uint32
	beacon types.Beacon
}

func (b *beaconAction) execute(trt *Tortoise) {
	trt.OnBeacon(types.EpochID(b.epoch), b.beacon)
}

// beacon accepts publish epoch and value of the beacon
func (s *session) beacon(epoch uint32, value string) *beaconAction {
	beacon := &beaconAction{epoch: epoch + 1}
	copy(beacon.beacon[:], value)
	s.register(beacon)
	return beacon
}

type results struct {
	results []result.Layer
}

func (r *results) next(lid uint32) *results {
	r.results = append(r.results, result.Layer{
		Layer:  types.GetEffectiveGenesis() + types.LayerID(lid),
		Blocks: []result.Block{},
	})
	return r
}

func (r *results) verified(lid uint32) *results {
	r.results = append(r.results, result.Layer{
		Layer:    types.GetEffectiveGenesis() + types.LayerID(lid),
		Verified: true,
		Blocks:   []result.Block{},
	})
	return r
}

const (
	hare = 1 << iota
	valid
	invalid
	data
)

func (r *results) block(id string, height uint64, fields uint) *results {
	rst := &r.results[len(r.results)-1]
	block := result.Block{
		Valid:   fields&valid > 0,
		Invalid: fields&invalid > 0,
		Hare:    fields&hare > 0,
		Data:    fields&data > 0,
	}
	copy(block.Header.ID[:], id)
	block.Header.LayerID = rst.Layer
	block.Header.Height = height
	rst.Blocks = append(rst.Blocks, block)
	return r
}

type updateActions struct {
	tb     testing.TB
	expect []result.Layer
}

func (u *updateActions) execute(trt *Tortoise) {
	u.tb.Helper()
	updates := trt.Updates()
	for i := range updates {
		// TODO(dshulyak) don't know yet how to implement
		updates[i].Opinion = types.Hash32{}
	}
	require.Equal(u.tb, u.expect, updates)
}

func (s *session) updates(tb testing.TB, expect *results) {
	s.register(&updateActions{tb, expect.results})
}

func (s *session) run(trt *Tortoise) {
	for _, a := range s.actions {
		a.execute(trt)
	}
}

func (s *session) ensureConfig() {
	if s.config == nil {
		config := DefaultConfig()
		s.config = &config
	}
}

func (s *session) hdist(val uint32) *session {
	s.ensureConfig()
	s.config.Hdist = val
	return s
}

func (s *session) zdist(val uint32) *session {
	s.ensureConfig()
	s.config.Zdist = val
	return s
}

func (s *session) window(val uint32) *session {
	s.ensureConfig()
	s.config.WindowSize = val
	return s
}

func (s *session) layersize(val uint32) *session {
	s.ensureConfig()
	s.config.LayerSize = val
	return s
}

func (s *session) delay(val uint32) *session {
	s.ensureConfig()
	s.config.BadBeaconVoteDelayLayers = val
	return s
}

func (s *session) tortoise(tb testing.TB) *Tortoise {
	s.ensureConfig()
	trt, err := New(WithLogger(logtest.New(tb)), WithConfig(*s.config))
	require.NoError(tb, err)
	return trt
}

func TestTortoise(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		var (
			s         = new(session)
			activeset []*atxAction
			n         = 5
		)
		for i := 0; i < n; i++ {
			activeset = append(
				activeset,
				s.smesher(i).atx(1, new(aopt).ticks(100).units(4)),
			)
		}
		s.beacon(1, "a")
		for i := 0; i < n; i++ {
			s.smesher(i).atx(1).ballot(1, new(bopt).
				beacon("a").
				activeset(activeset...).
				eligibilities(50/n))
		}
		s.tally(1)
		s.hareblock(1, "aa", 0)
		for i := 0; i < n-1; i++ {
			s.smesher(i).atx(1).ballot(2, new(bopt).
				eligibilities(50/n),
			)
		}
		s.tally(2)
		s.updates(t, new(results).
			next(1).block("aa", 0, hare|data).
			next(2),
		)
		s.run(s.tortoise(t))
	})
}
