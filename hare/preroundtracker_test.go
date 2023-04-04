package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	k              = 1
	ki             = preRound
	lowThresh10    = 10
	lowDefaultSize = 100
)

func genLayerProposal(layerID types.LayerID, txs []types.TransactionID) *types.Proposal {
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				BallotMetadata: types.BallotMetadata{
					Layer: layerID,
				},
				InnerBallot: types.InnerBallot{
					AtxID: types.RandomATXID(),
					EpochData: &types.EpochData{
						Beacon: types.RandomBeacon(),
					},
				},
			},
			TxIDs: txs,
		},
	}
	signer, _ := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.BALLOT, p.SignedBytes())
	p.Initialize()
	return p
}

func BuildPreRoundMsg(sig *signing.EdSigner, s *Set, roleProof types.VrfSignature) *Msg {
	builder := newMessageBuilder()
	builder.SetType(pre).SetLayer(instanceID1).SetRoundCounter(preRound).SetCommittedRound(ki).SetValues(s).SetRoleProof(roleProof)
	builder.SetEligibilityCount(1)
	return builder.SetNodeID(sig.NodeID()).Sign(sig).Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	s.Add(types.ProposalID{2})
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, lowThresh10, lowThresh10)

	m1 := BuildPreRoundMsg(signer, s, types.EmptyVrfSignature)
	et.Track(m1.NodeID, m1.Round, m1.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), m1)
	require.Equal(t, 1, len(tracker.preRound))      // one msg
	require.Equal(t, 2, len(tracker.tracker.table)) // two Values
	g := tracker.preRound[signer.NodeID()]
	require.True(t, s.Equals(g.Set))
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{1}).hCount)
	nSet := NewSetFromValues(types.ProposalID{3}, types.ProposalID{4})
	m2 := BuildPreRoundMsg(signer, nSet, types.EmptyVrfSignature)
	m2.Eligibility.Count = 2
	et.Track(m2.NodeID, m2.Round, m2.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), m2)
	h := tracker.preRound[signer.NodeID()]
	require.True(t, h.Equals(s.Union(nSet)))
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg:  m1.HareMetadata,
							Signature: m1.Signature,
						},
						{
							InnerMsg:  m2.HareMetadata,
							Signature: m2.Signature,
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       m2.Layer,
			Round:       m2.Round,
			NodeID:      m2.NodeID,
			Eligibility: m2.Eligibility,
		},
	}
	gossip := <-mch
	require.Equal(t, expected, *gossip)

	interSet := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{5})
	m3 := BuildPreRoundMsg(signer, interSet, types.EmptyVrfSignature)
	et.Track(m3.NodeID, m3.Round, m3.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), m3)
	h = tracker.preRound[signer.NodeID()]
	require.True(t, h.Equals(s.Union(nSet).Union(interSet)))
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{1}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{2}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{3}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{4}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{5}).hCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{1}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{2}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{3}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{4}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{5}).dhCount)
	require.Len(t, mch, 1)

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	s4 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{5})
	m4 := BuildPreRoundMsg(signer2, s4, types.EmptyVrfSignature)
	et.Track(m4.NodeID, m4.Round, m4.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), m4)
	h = tracker.preRound[signer2.NodeID()]
	require.True(t, h.Equals(s4))
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{1}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{2}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{3}).hCount)
	require.Equal(t, 0, tracker.tracker.CountStatus(types.ProposalID{4}).hCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{5}).hCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{1}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{2}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{3}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{4}).dhCount)
	require.Equal(t, 1, tracker.tracker.CountStatus(types.ProposalID{5}).dhCount)
}

func TestPreRoundTracker_CanProveValueAndSet(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, lowThresh10, lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		require.False(t, tracker.CanProveSet(s))
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		m1 := BuildPreRoundMsg(sig, s, types.EmptyVrfSignature)
		et.Track(m1.NodeID, m1.Round, m1.Eligibility.Count, true)
		tracker.OnPreRound(context.Background(), m1)
	}

	require.True(t, tracker.CanProveValue(types.ProposalID{1}))
	require.True(t, tracker.CanProveValue(types.ProposalID{2}))
	require.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_CanProveValueAndSet_WithKnownEquivocators(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, lowThresh10, lowThresh10)

	for i := 0; i < lowThresh10-1; i++ {
		require.False(t, tracker.CanProveSet(s))
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildPreRoundMsg(sig, s, types.EmptyVrfSignature)
		et.Track(m.NodeID, m.Round, m.Eligibility.Count, true)
		tracker.OnPreRound(context.Background(), m)
	}

	require.False(t, tracker.CanProveValue(types.ProposalID{1}))
	require.False(t, tracker.CanProveValue(types.ProposalID{2}))
	require.False(t, tracker.CanProveSet(s))

	// add a known equivocator
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	et.Track(sig.NodeID(), preRound, 1, false)
	require.True(t, tracker.CanProveValue(types.ProposalID{1}))
	require.True(t, tracker.CanProveValue(types.ProposalID{2}))
	require.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_CanProveValueAndSet_TooFewKnownEquivocators(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, lowThresh10+1, lowThresh10*2)

	// add a known equivocator
	for i := 0; i < lowThresh10-1; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), preRound, 1, false)
	}

	require.False(t, tracker.CanProveSet(s))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(sig, s, types.EmptyVrfSignature)
	et.Track(m.NodeID, m.Round, m.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), m)

	require.False(t, tracker.CanProveValue(types.ProposalID{1}))
	require.False(t, tracker.CanProveValue(types.ProposalID{2}))
	require.False(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_CanProveValueAndSet_TooHonestVotes(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, lowThresh10+1, lowThresh10*2)

	// add a known equivocator
	for i := 0; i < lowThresh10*2; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), preRound, 1, false)
	}

	require.False(t, tracker.CanProveValue(types.ProposalID{1}))
	require.False(t, tracker.CanProveValue(types.ProposalID{2}))
	require.False(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_CanProveValueAndSet_JustEnough(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, lowThresh10+1, lowThresh10*2)

	// add a known equivocator
	for i := 0; i < lowThresh10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), preRound, 1, false)
	}

	require.False(t, tracker.CanProveSet(s))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(sig, s, types.EmptyVrfSignature)
	et.Track(m.NodeID, m.Round, m.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), m)

	require.True(t, tracker.CanProveValue(types.ProposalID{1}))
	require.True(t, tracker.CanProveValue(types.ProposalID{2}))
	require.True(t, tracker.CanProveSet(s))
}

func TestPreRoundTracker_UpdateSet(t *testing.T) {
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, 2, 2)
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{4})
	prMsg1 := BuildPreRoundMsg(sig1, s1, types.EmptyVrfSignature)
	et.Track(prMsg1.NodeID, prMsg1.Round, prMsg1.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), prMsg1)
	prMsg2 := BuildPreRoundMsg(sig2, s2, types.EmptyVrfSignature)
	et.Track(prMsg2.NodeID, prMsg2.Round, prMsg2.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), prMsg2)
	require.True(t, tracker.CanProveValue(types.ProposalID{1}))
	require.True(t, tracker.CanProveValue(types.ProposalID{2}))
	require.False(t, tracker.CanProveSet(s1))
	require.False(t, tracker.CanProveSet(s2))
	require.Empty(t, mch)
}

func TestPreRoundTracker_OnPreRound2(t *testing.T) {
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, 2, 2)
	s1 := NewSetFromValues(types.ProposalID{1})
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	prMsg1 := BuildPreRoundMsg(sig, s1, types.EmptyVrfSignature)
	et.Track(prMsg1.NodeID, prMsg1.Round, prMsg1.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), prMsg1)
	require.Equal(t, 1, len(tracker.preRound))
	prMsg2 := BuildPreRoundMsg(sig, s1, types.EmptyVrfSignature)
	et.Track(prMsg2.NodeID, prMsg2.Round, prMsg2.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), prMsg2)
	require.Equal(t, 1, len(tracker.preRound))
	require.Empty(t, mch)
}

func TestPreRoundTracker_FilterSet(t *testing.T) {
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newPreRoundTracker(logtest.New(t), mch, et, 2, 2)
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	prMsg1 := BuildPreRoundMsg(sig1, s1, types.EmptyVrfSignature)
	et.Track(prMsg1.NodeID, prMsg1.Round, prMsg1.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), prMsg1)
	prMsg2 := BuildPreRoundMsg(sig2, s1, types.EmptyVrfSignature)
	et.Track(prMsg2.NodeID, prMsg2.Round, prMsg2.Eligibility.Count, true)
	tracker.OnPreRound(context.Background(), prMsg2)
	set := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	tracker.FilterSet(set)
	require.True(t, set.Equals(s1))
	require.Empty(t, mch)
}

func TestPreRoundTracker_BestVRF(t *testing.T) {
	r := require.New(t)

	var (
		sig411 = types.VrfSignature{ // coinflip false
			0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,

			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x04,
		}

		sig322 = types.VrfSignature{ // coinflip true
			0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,

			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x02, 0x03,
		}

		sig177 = types.VrfSignature{ // coinflip true
			0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,

			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x07, 0x07, 0x01,
		}

		sig0a7 = types.VrfSignature{ // coinflip false
			0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,

			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x07, 0x0a, 0x00,
		}
	)

	values := []struct {
		proof   types.VrfSignature
		bestVal types.VrfSignature
		coin    bool
	}{
		// order matters! lowest VRF value wins
		// first is input bytes, second is lowest val seen so far,
		// fourth is lowest-order bit (big endian) of lowest value
		{sig411, sig411, false},
		{sig322, sig322, true},
		{sig177, sig177, true},
		{sig411, sig177, true},
		{sig322, sig177, true},
		{sig0a7, sig0a7, false},
	}

	// check default coin value
	et := NewEligibilityTracker(2)
	tracker := newPreRoundTracker(logtest.New(t), make(chan *types.MalfeasanceGossip, 2), et, 2, 2)
	r.False(tracker.coinflip, "expected initial coinflip value to be false")
	r.Nil(tracker.bestVRF, "expected initial best VRF to be nil")
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})

	for _, v := range values {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		prMsg := BuildPreRoundMsg(sig, s1, v.proof)
		tracker.OnPreRound(context.Background(), prMsg)
		r.Equalf(v.bestVal, *tracker.bestVRF, "mismatch in best VRF value for input %s", v.proof)
		r.Equalf(v.coin, tracker.coinflip, "mismatch in weak coin flip for input %s", v.proof)
	}
}
