package hare

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	k              = 1
	ki             = preRound
	lowThresh10    = 10
	lowDefaultSize = 100
)

func getProposalID(i int) types.ProposalID {
	return genLayerProposal(types.NewLayerID(1), nil).ID()
}

func genLayerProposal(layerID types.LayerID, txs []types.TransactionID) *types.Proposal {
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					AtxID:      types.RandomATXID(),
					LayerIndex: layerID,
					EpochData: &types.EpochData{
						ActiveSet: types.RandomActiveSet(10),
						Beacon:    types.RandomBeacon(),
					},
				},
			},
			TxIDs: txs,
		},
	}
	signer, _ := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	p.Initialize()
	return p
}

var (
	value1  = getProposalID(1)
	value2  = getProposalID(2)
	value3  = getProposalID(3)
	value4  = getProposalID(4)
	value5  = getProposalID(5)
	value6  = getProposalID(6)
	value7  = getProposalID(7)
	value8  = getProposalID(8)
	value9  = getProposalID(9)
	value10 = getProposalID(10)
)

func BuildPreRoundMsg(signing Signer, s *Set, roleProof []byte) *Msg {
	builder := newMessageBuilder()
	builder.SetType(pre).SetInstanceID(instanceID1).SetRoundCounter(k).SetKi(ki).SetValues(s).SetRoleProof(roleProof)
	builder.SetPubKey(signing.PublicKey())
	builder.SetEligibilityCount(1)

	return builder.Sign(signing).Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m1 := BuildPreRoundMsg(signer, s, nil)
	tracker := newPreRoundTracker(lowThresh10, lowThresh10, logtest.New(t))
	tracker.OnPreRound(context.Background(), m1)
	assert.Equal(t, 1, len(tracker.preRound))      // one msg
	assert.Equal(t, 2, len(tracker.tracker.table)) // two Values
	g := tracker.preRound[signer.PublicKey().String()]
	assert.True(t, s.Equals(g))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value1))
	nSet := NewSetFromValues(value3, value4)
	m2 := BuildPreRoundMsg(signer, nSet, nil)
	m2.InnerMsg.EligibilityCount = 2
	tracker.OnPreRound(context.Background(), m2)
	h := tracker.preRound[signer.PublicKey().String()]
	assert.True(t, h.Equals(s.Union(nSet)))

	interSet := NewSetFromValues(value1, value2, value5)
	m3 := BuildPreRoundMsg(signer, interSet, nil)
	tracker.OnPreRound(context.Background(), m3)
	h = tracker.preRound[signer.PublicKey().String()]
	assert.True(t, h.Equals(s.Union(nSet).Union(interSet)))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value1))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value2))
	assert.EqualValues(t, 2, tracker.tracker.CountStatus(value3))
	assert.EqualValues(t, 2, tracker.tracker.CountStatus(value4))
	assert.EqualValues(t, 1, tracker.tracker.CountStatus(value5))
}

func TestPreRoundTracker_CanProveValueAndSet(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	tracker := newPreRoundTracker(lowThresh10, lowThresh10, logtest.New(t))

	for i := 0; i < lowThresh10; i++ {
		assert.False(t, tracker.CanProveSet(s))
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		m1 := BuildPreRoundMsg(sig, s, nil)
		tracker.OnPreRound(context.Background(), m1)
	}

	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_UpdateSet(t *testing.T) {
	tracker := newPreRoundTracker(2, 2, logtest.New(t))
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	s1 := NewSetFromValues(value1, value2, value3)
	s2 := NewSetFromValues(value1, value2, value4)
	prMsg1 := BuildPreRoundMsg(sig1, s1, nil)
	tracker.OnPreRound(context.Background(), prMsg1)
	prMsg2 := BuildPreRoundMsg(sig2, s2, nil)
	tracker.OnPreRound(context.Background(), prMsg2)
	assert.True(t, tracker.CanProveValue(value1))
	assert.True(t, tracker.CanProveValue(value2))
	assert.False(t, tracker.CanProveSet(s1))
	assert.False(t, tracker.CanProveSet(s2))
}

func TestPreRoundTracker_OnPreRound2(t *testing.T) {
	tracker := newPreRoundTracker(2, 2, logtest.New(t))
	s1 := NewSetFromValues(value1)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	prMsg1 := BuildPreRoundMsg(sig, s1, nil)
	tracker.OnPreRound(context.Background(), prMsg1)
	assert.Equal(t, 1, len(tracker.preRound))
	prMsg2 := BuildPreRoundMsg(sig, s1, nil)
	tracker.OnPreRound(context.Background(), prMsg2)
	assert.Equal(t, 1, len(tracker.preRound))
}

func TestPreRoundTracker_FilterSet(t *testing.T) {
	tracker := newPreRoundTracker(2, 2, logtest.New(t))
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	s1 := NewSetFromValues(value1, value2)
	prMsg1 := BuildPreRoundMsg(sig1, s1, nil)
	tracker.OnPreRound(context.Background(), prMsg1)
	prMsg2 := BuildPreRoundMsg(sig2, s1, nil)
	tracker.OnPreRound(context.Background(), prMsg2)
	set := NewSetFromValues(value1, value2, value3)
	tracker.FilterSet(set)
	assert.True(t, set.Equals(s1))
}

func TestPreRoundTracker_BestVRF(t *testing.T) {
	r := require.New(t)

	values := []struct {
		proof   []byte
		val     uint32
		bestVal uint32
		coin    bool
	}{
		// order matters! lowest VRF value wins
		// first is input bytes, second is output sha256 checksum as uint32 (lowest order four bytes),
		// third is lowest val seen so far, fourth is lowest-order bit of lowest value
		{[]byte{0}, 2617980014, 2617980014, false},
		{[]byte{1}, 789771595, 789771595, true},
		{[]byte{2}, 3384066523, 789771595, true},
		{[]byte{3}, 149769992, 149769992, false},
		{[]byte{4}, 1352412645, 149769992, false},
		{[]byte{1, 0}, 206888007, 149769992, false},
		{[]byte{1, 0, 0}, 131879163, 131879163, true},
	}

	// check default coin value
	tracker := newPreRoundTracker(2, 2, logtest.New(t))
	r.False(tracker.coinflip, "expected initial coinflip value to be false")
	r.Equal(tracker.bestVRF, uint32(math.MaxUint32), "expected initial best VRF to be max uint32")
	s1 := NewSetFromValues(value1, value2)

	for _, v := range values {
		sha := hash.Sum(v.proof)
		shaUint32 := binary.LittleEndian.Uint32(sha[:4])
		r.Equal(v.val, shaUint32, "mismatch in hash output")
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		prMsg := BuildPreRoundMsg(sig, s1, v.proof)
		tracker.OnPreRound(context.Background(), prMsg)
		r.Equal(v.bestVal, tracker.bestVRF, "mismatch in best VRF value")
		r.Equal(v.coin, tracker.coinflip, "mismatch in weak coin flip")
	}
}
