package tortoise

import (
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

type smesher struct {
	id string

	defaults types.ActivationTxHeader

	atxs map[uint32]*atx
}

type atxOpt func(*types.ActivationTxHeader)

func base(value uint64) atxOpt {
	return func(header *types.ActivationTxHeader) {
		header.BaseTickHeight = value
	}
}

func ticks(value uint64) atxOpt {
	return func(header *types.ActivationTxHeader) {
		header.TickCount = value
	}
}

func units(value uint32) atxOpt {
	return func(header *types.ActivationTxHeader) {
		header.EffectiveNumUnits = value
	}
}

func (s smesher) atx(epoch uint32, opts ...atxOpt) *atx {
	if val, exists := s.atxs[epoch]; exists {
		return val
	}
	header := s.defaults
	header.ID = hash.Sum([]byte(s.id), []byte(strconv.Itoa(int(epoch))))
	header.PublishEpoch = types.EpochID(epoch + 1)
	for _, opt := range opts {
		opt(&header)
	}
	val := &atx{header: header, ballots: map[uint32]*ballot{}}
	s.atxs[epoch] = val
	return val
}

type atx struct {
	header    types.ActivationTxHeader
	ballots   map[uint32]*ballot
	reference *ballot
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

func activeset(values ...atx) ballotOpt {
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

func (a *atx) ballot(lid uint32, opts ...ballotOpt) *ballot {
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
	val := &ballot{ballot: b}
	if a.reference == nil {
		a.reference = val
	} else {
		b.RefBallot = a.reference.ballot.ID()
	}
	a.ballots[lid] = val
	return val
}

type ballot struct {
	ballot types.Ballot
}

func tbeacon(value string) (val types.Beacon) {
	copy(val[:], value)
	return val
}

type cluster struct {
	smeshers map[string]*smesher
}

func (c *cluster) get(id string) *smesher {
	if val, exist := c.smeshers[id]; exist {
		return val
	}
	val := &smesher{id: id, atxs: map[uint32]*atx{}}
	c.smeshers[id] = val
	return val
}

func run(actions ...any) func(t *testing.T) {
	return func(t *testing.T) {

	}
}

func TestTortoise(t *testing.T) {
	t.Run("sanity", run())
}
