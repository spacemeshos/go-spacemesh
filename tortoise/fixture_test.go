package tortoise

import (
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/stretchr/testify/require"
)

type smesher struct {
	id string

	defaults types.ActivationTxHeader

	atxs map[uint32]*atxAction
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

func (s *smesher) atx(epoch uint32, opts ...*aopt) *atxAction {
	if val, exists := s.atxs[epoch]; exists {
		return val
	}
	header := s.defaults
	header.ID = hash.Sum([]byte(s.id), []byte(strconv.Itoa(int(epoch))))
	header.PublishEpoch = types.EpochID(epoch)
	for _, opt := range opts {
		for _, o := range opt.opts {
			o(&header)
		}
	}
	val := &atxAction{
		header:  header,
		ballots: map[uint32]*ballotAction{},
	}
	s.atxs[epoch] = val
	return val
}

type atxAction struct {
	session *session

	header    types.ActivationTxHeader
	ballots   map[uint32]*ballotAction
	reference *ballotAction
}

type ballotOpt func(*types.Ballot)

func beacon(value string) ballotOpt {
	return func(ballot *types.Ballot) {
		if ballot.EpochData == nil {
			ballot.EpochData = &types.EpochData{}
		}
		copy(ballot.EpochData.Beacon[:], value)
	}
}

func eligibilities(value int) ballotOpt {
	return func(ballot *types.Ballot) {
		ballot.EligibilityProofs = make([]types.VotingEligibility, value)
	}
}

func activeset(values ...atxAction) ballotOpt {
	return func(ballot *types.Ballot) {
		for _, val := range values {
			ballot.ActiveSet = append(ballot.ActiveSet, val.header.ID)
		}
	}
}

func bvotes(value types.Votes) ballotOpt {
	return func(ballot *types.Ballot) {
		ballot.Votes = value
	}
}

func malicious() ballotOpt {
	return func(ballot *types.Ballot) {
		ballot.SetMalicious()
	}
}

func (a *atxAction) ballot(lid uint32, opts ...ballotOpt) *ballotAction {
	lid = lid + types.GetEffectiveGenesis().Uint32()
	if val, exist := a.ballots[lid]; exist {
		return val
	}
	b := types.Ballot{}
	b.Layer = types.LayerID(lid)
	hs := hash.Sum(a.header.ID[:], []byte(strconv.Itoa(int(lid))))
	id := types.BallotID{}
	copy(id[:], hs[:])
	b.SetID(id)
	for _, opt := range opts {
		opt(&b)
	}
	val := &ballotAction{ballot: b}
	if a.reference == nil {
		a.reference = val
	} else {
		b.RefBallot = a.reference.ballot.ID()
	}
	a.ballots[lid] = val
	return val
}

type ballotAction struct {
	ballot types.Ballot
}

type session struct {
	smeshers map[string]*smesher
	actions  []any
}

func (s *session) smesher(id string) *smesher {
	if s.smeshers == nil {
		s.smeshers = map[string]*smesher{}
	}
	if val, exist := s.smeshers[id]; exist {
		return val
	}
	val := &smesher{id: id, atxs: map[uint32]*atxAction{}}
	s.smeshers[id] = val
	return val
}

func (s *session) append(actions ...any) {
	s.actions = append(s.actions, actions...)
}

func (s *session) run(t *Tortoise) {
	for _, a := range s.actions {
		switch typed := a.(type) {
		case *beaconAction:
			t.OnBeacon(types.EpochID(typed.epoch), typed.beacon)
		case *ballotAction:
			t.OnBallot(&typed.ballot)
		case *atxAction:
			t.OnAtx(&typed.header)
		}
	}
}

type beaconAction struct {
	epoch  uint32
	beacon types.Beacon
}

func (s *session) beacon(epoch uint32, value string) *beaconAction {
	beacon := &beaconAction{epoch: epoch}
	copy(beacon.beacon[:], value)
	return beacon
}

func mustNew(tb testing.TB, opts ...Opt) *Tortoise {
	t, err := New(append(opts, WithLogger(logtest.New(tb)))...)
	require.NoError(tb, err)
	return t
}

func TestTortoise(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		var s session
		s.append(
			s.smesher("1").atx(1, new(aopt).base(0).ticks(1).units(1)),
			s.smesher("2").atx(1, new(aopt).base(0).ticks(1).units(1)),
			s.smesher("3").atx(1, new(aopt).base(0).ticks(1).units(1)),
			s.beacon(1, "a"),
			s.smesher("1").atx(1).ballot(1),
			s.smesher("1").atx(1).ballot(1),
			s.smesher("1").atx(2).ballot(1),
		)
		s.run(mustNew(t))
	})
}
