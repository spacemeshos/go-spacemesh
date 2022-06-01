package proposals

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	defaultATXUnit = uint(5)
	testedATXUnit  = uint(2)
	// eligibleSlots is calculated based on layerAvgSize, layersPerEpoch, epoch ATX weight and smesher's own weight.
	eligibleSlots = uint32(3)
	epoch         = types.EpochID(3)
)

func genActiveSet() types.ATXIDList {
	return types.ATXIDList{types.RandomATXID(), types.RandomATXID(), types.RandomATXID(), types.RandomATXID()}
}

func genSigner() *signing.EdSigner {
	return signing.NewEdSignerFromRand(rand.New(rand.NewSource(1001)))
}

type testValidator struct {
	*Validator
	*mockSet
}

func createTestValidator(tb testing.TB) *testValidator {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(tb)
	return &testValidator{
		Validator: NewEligibilityValidator(layerAvgSize, layersPerEpoch, ms.mdb, ms.mbc, ms.mm, logtest.New(tb)),
		mockSet:   ms,
	}
}

func createBallots(tb testing.TB, signer *signing.EdSigner, vrfSigner *signing.VRFSigner, activeSet types.ATXIDList, beacon types.Beacon) []*types.Ballot {
	totalWeight := uint64(len(activeSet)-1)*uint64(defaultATXUnit) + uint64(testedATXUnit)
	slots, err := GetNumEligibleSlots(uint64(testedATXUnit), totalWeight, layerAvgSize, layersPerEpoch)
	require.NoError(tb, err)
	require.Equal(tb, eligibleSlots, slots)
	eligibilityProofs := map[types.LayerID][]types.VotingEligibilityProof{}
	order := make([]types.LayerID, 0, eligibleSlots)
	for counter := uint32(0); counter < eligibleSlots; counter++ {
		message, err := SerializeVRFMessage(beacon, epoch, counter)
		require.NoError(tb, err)
		vrfSig := vrfSigner.Sign(message)
		eligibleLayer := CalcEligibleLayer(epoch, layersPerEpoch, vrfSig)
		if _, exist := eligibilityProofs[eligibleLayer]; !exist {
			order = append(order, eligibleLayer)
		}
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
	}
	sort.Slice(order, func(i, j int) bool {
		return order[i].Before(order[j])
	})
	ballots := make([]*types.Ballot, 0, eligibleSlots)
	for _, lyr := range order {
		proofs := eligibilityProofs[lyr]
		isRef := len(ballots) == 0
		b := types.RandomBallot()
		b.AtxID = activeSet[0]
		b.LayerIndex = lyr
		if isRef {
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{
				ActiveSet: activeSet,
				Beacon:    beacon,
			}
		} else {
			b.RefBallot = ballots[0].ID()
		}
		b.EligibilityProofs = proofs
		b.Signature = signer.Sign(b.Bytes())
		require.NoError(tb, b.Initialize())
		ballots = append(ballots, b)
	}
	return ballots
}

func TestCheckEligibility_FailedToGetRefBallot(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	vrfSigner := signer.VRFSigner()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	errUnknown := errors.New("unknown")
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(nil, errUnknown).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errUnknown)
	assert.False(t, eligible)
}

func TestCheckEligibility_RefBallotMissingEpochData(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	vrfSigner := signer.VRFSigner()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	rb.EpochData = nil
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errMissingEpochData)
	assert.False(t, eligible)
}

func TestCheckEligibility_RefBallotMissingBeacon(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	vrfSigner := signer.VRFSigner()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	rb.EpochData.Beacon = types.EmptyBeacon
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errMissingBeacon)
	assert.False(t, eligible)
}

func TestCheckEligibility_RefBallotEmptyActiveSet(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	vrfSigner := signer.VRFSigner()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	rb.EpochData.ActiveSet = nil
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errEmptyActiveSet)
	assert.False(t, eligible)
}

func TestCheckEligibility_FailToGetActiveSetATXHeader(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	vrfSigner := signer.VRFSigner()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	activeSet := rb.EpochData.ActiveSet
	errUnknown := errors.New("unknown")
	tv.mdb.EXPECT().GetAtxHeader(activeSet[0]).Return(nil, errUnknown).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errUnknown)
	assert.False(t, eligible)
}

func TestCheckEligibility_FailToGetBallotATXHeader(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	vrfSigner := signer.VRFSigner()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	b := ballots[1]
	errUnknown := errors.New("unknown")
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(nil, errUnknown).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errUnknown)
	assert.False(t, eligible)
}

func TestCheckEligibility_TargetEpochMismatch(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer(),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errTargetEpochMismatch)
	assert.False(t, eligible)
}

func TestCheckEligibility_KeyMismatch(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.NodeID{1},
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errPublicKeyMismatch)
	assert.False(t, eligible)
}

func TestCheckEligibility_ZeroTotalWeight(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: 0,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, ErrZeroTotalWeight)
	assert.False(t, eligible)
}

func TestCheckEligibility_BadCounter(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	b.EligibilityProofs[0].J = b.EligibilityProofs[0].J + 100
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errIncorrectCounter)
	assert.False(t, eligible)
}

func TestCheckEligibility_InvalidOrder(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSignerFromRand(rand.New(rand.NewSource(2222)))
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{7, 7, 7})
	rb := ballots[0]
	require.Len(t, rb.EligibilityProofs, 2)
	hdr := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	hdr.SetID(&rb.AtxID)
	for _, id := range rb.EpochData.ActiveSet {
		if id == rb.AtxID {
			tv.mdb.EXPECT().GetAtxHeader(id).Return(hdr, nil).Times(2)
			continue
		}
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(2)
	}
	tv.mdb.EXPECT().GetAtxHeader(rb.AtxID).Return(hdr, nil).Times(2)

	rb.EligibilityProofs[0], rb.EligibilityProofs[1] = rb.EligibilityProofs[1], rb.EligibilityProofs[0]
	eligible, err := tv.CheckEligibility(context.TODO(), rb)
	assert.ErrorIs(t, err, errInvalidProofsOrder)
	assert.False(t, eligible)

	rb.EligibilityProofs[0], rb.EligibilityProofs[1] = rb.EligibilityProofs[1], rb.EligibilityProofs[0]
	rb.EligibilityProofs = append(rb.EligibilityProofs, types.VotingEligibilityProof{J: 2})
	eligible, err = tv.CheckEligibility(context.TODO(), rb)
	assert.ErrorIs(t, err, errIncorrectVRFSig)
	assert.False(t, eligible)
}

func TestCheckEligibility_BadVRFSignature(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	b.EligibilityProofs[0].Sig = b.EligibilityProofs[0].Sig[1:]
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errIncorrectVRFSig)
	assert.False(t, eligible)
}

func TestCheckEligibility_IncorrectLayerIndex(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), types.Beacon{1, 1, 1})
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	b.EligibilityProofs[0].Sig = b.EligibilityProofs[0].Sig[1:]
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errIncorrectVRFSig)
	assert.False(t, eligible)
}

func TestCheckEligibility(t *testing.T) {
	tv := createTestValidator(t)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	rb := ballots[0]
	hdr := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: testedATXUnit,
	}
	hdr.SetID(&rb.AtxID)

	for _, id := range rb.EpochData.ActiveSet {
		if id == rb.AtxID {
			tv.mdb.EXPECT().GetAtxHeader(id).Return(hdr, nil).Times(len(ballots))
			continue
		}
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(len(ballots))
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(len(ballots) - 1)
	for _, b := range ballots {
		tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(hdr, nil).Times(1)
		tv.mbc.EXPECT().ReportBeaconFromBallot(epoch, b.ID(), beacon, hdr.GetWeight()).Times(1)
		eligible, err := tv.CheckEligibility(context.TODO(), b)
		assert.NoError(t, err)
		assert.True(t, eligible)
	}
}
