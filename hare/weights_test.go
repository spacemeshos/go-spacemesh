package hare

import (
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"testing"
)

type weightedNode struct {
	weight uint64
	signer *signing.EdSigner
}

type weighted []weightedNode

func genWeighted(count int) weighted {
	wd := make(weighted,count)
	for i := range wd {
		wd[i].weight = uint64(rand.Intn(100))
		wd[i].signer = signing.NewEdSigner()
	}
	return wd
}

func (wd weighted) getCommittee() Committee {
	actives := map[string]uint64{}
	for _,w := range wd {
		actives[w.signer.PublicKey().String()] = w.weight
	}
	return Committee{
		actives: actives,
		Weight: calcCommitteeWeight(actives, len(wd)),
	}
}

func TestWightedStatusTracker_RecordStatus_Ready(t *testing.T) {
	wd := genWeighted(lowThresh10)
	tracker := newStatusTracker(wd.getCommittee())

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		tracker.AnalyzeStatuses(func(*Msg)bool{return true})
		assert.False(t, tracker.IsSVPReady())
		tracker.RecordStatus(BuildPreRoundMsg(wd[i].signer, s))
	}

	tracker.AnalyzeStatuses(func(*Msg)bool{return true})
	assert.True(t, tracker.IsSVPReady())
}

func TestWightedStatusTracker_RecordStatus_NotReady(t *testing.T) {
	wd := genWeighted(lowThresh10)
	tracker := newStatusTracker(wd.getCommittee())

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10 - 1; i++ {
		tracker.AnalyzeStatuses(func(*Msg)bool{return true})
		assert.False(t, tracker.IsSVPReady())
		tracker.RecordStatus(BuildPreRoundMsg(wd[i].signer, s))
	}

	tracker.AnalyzeStatuses(func(*Msg)bool{return true})
	assert.False(t, tracker.IsSVPReady())
}

func TestWeightTracker_Track(t *testing.T) {
	type id uint32
	tracker := WeightTracker{}
	mi1 := id(1)
	tracker.Track(mi1,1)
	assert.Equal(t, 1, len(tracker))
	mi2 := MyInt{2}
	tracker.Track(mi2, 2)
	assert.Equal(t, 2, len(tracker))
}

func TestWeightTracker_WeightOf(t *testing.T) {
	type id uint32
	tracker := WeightTracker{}
	assert.Equal(t, uint64(0), tracker.WeightOf(id(1)))
	tracker.Track(id(1),1)
	assert.Equal(t, uint64(1), tracker.WeightOf(id(1)))
	tracker.Track(id(1),2)
	assert.Equal(t, uint64(3), tracker.WeightOf(id(1)))
}
