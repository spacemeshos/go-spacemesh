package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	defaultUnit = uint(5)
	ballotUnit  = uint(1)
	// eligibleSlots is calculated based on layerAvgSize, layersPerEpoch, epoch ATX weight and smesher's own weight.
	eligibleSlots = uint32(3)
	epoch         = types.EpochID(3)
)

func genActiveSet() types.ATXIDList {
	return types.ATXIDList{types.RandomATXID(), types.RandomATXID()}
}

func genBeacon() []byte {
	return types.RandomBytes(types.BeaconSize)
}

type testValidator struct {
	*validator
	*mockSet
}

func createTestValidator(tb testing.TB) *testValidator {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(tb)
	return &testValidator{
		validator: newEligibilityValidator(layerAvgSize, layersPerEpoch, ms.mdb, ms.mbc, ms.mm, logtest.New(tb)),
		mockSet:   ms,
	}
}

func createBallots(tb testing.TB, signer *signing.EdSigner, vrfSigner *signing.VRFSigner, activeSet types.ATXIDList, beacon []byte) []*types.Ballot {
	slots, err := GetNumEligibleSlots(uint64(ballotUnit), uint64(int(defaultUnit)*len(activeSet)), layerAvgSize, layersPerEpoch)
	require.NoError(tb, err)
	require.Equal(tb, eligibleSlots, slots)
	eligibilityProofs := map[types.LayerID][]types.VotingEligibilityProof{}
	for counter := uint32(0); counter < eligibleSlots; counter++ {
		message, err := SerializeVRFMessage(beacon, epoch, counter)
		require.NoError(tb, err)
		vrfSig := vrfSigner.Sign(message)
		eligibleLayer := CalcEligibleLayer(epoch, layersPerEpoch, vrfSig)
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibilityProof{
			J:   counter,
			Sig: vrfSig,
		})
	}
	ballots := make([]*types.Ballot, 0, eligibleSlots)
	for lyr, proofs := range eligibilityProofs {
		for _, p := range proofs {
			isRef := len(ballots) == 0
			b := types.RandomBallot()
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
			b.EligibilityProof = p
			b.Signature = signer.Sign(b.Bytes())
			require.NoError(tb, b.Initialize())
			ballots = append(ballots, b)
		}
	}
	return ballots
}

func TestCheckEligibility_FailedToGetRefBallot(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	errUnknown := errors.New("unknown")
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(nil, errUnknown).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errUnknown)
	assert.False(t, eligible)
}

func TestCheckEligibility_RefBallotMissingEpochData(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	rb.EpochData = nil
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errMissingEpochData)
	assert.False(t, eligible)
}

func TestCheckEligibility_RefBallotMissingBeacon(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	rb.EpochData.Beacon = nil
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errMissingBeacon)
	assert.False(t, eligible)
}

func TestCheckEligibility_RefBallotEmptyActiveSet(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	rb.EpochData.ActiveSet = nil
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), ballots[1])
	assert.ErrorIs(t, err, errEmptyActiveSet)
	assert.False(t, eligible)
}

func TestCheckEligibility_FailToGetActiveSetATXHeader(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
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
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
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
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID: types.NodeID{
				Key:          signer.PublicKey().String(),
				VRFPublicKey: vrfPubkey,
			},
			PubLayerID: epoch.FirstLayer(),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: ballotUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errTargetEpochMismatch)
	assert.False(t, eligible)
}

func TestCheckEligibility_KeyMismatch(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID: types.NodeID{
				Key:          "bad key",
				VRFPublicKey: vrfPubkey,
			},
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: ballotUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errPublicKeyMismatch)
	assert.False(t, eligible)
}

func TestCheckEligibility_ZeroTotalWeight(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
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
			NodeID: types.NodeID{
				Key:          signer.PublicKey().String(),
				VRFPublicKey: vrfPubkey,
			},
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: ballotUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, ErrZeroTotalWeight)
	assert.False(t, eligible)
}

func TestCheckEligibility_BadCounter(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID: types.NodeID{
				Key:          signer.PublicKey().String(),
				VRFPublicKey: vrfPubkey,
			},
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: ballotUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	b.EligibilityProof.J = b.EligibilityProof.J + 100
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errIncorrectCounter)
	assert.False(t, eligible)
}

func TestCheckEligibility_BadVRFSignature(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID: types.NodeID{
				Key:          signer.PublicKey().String(),
				VRFPublicKey: vrfPubkey,
			},
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: ballotUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	b.EligibilityProof.Sig = b.EligibilityProof.Sig[1:]
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errIncorrectVRFSig)
	assert.False(t, eligible)
}

func TestCheckEligibility_IncorrectLayerIndex(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), genBeacon())
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(1)
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(1)

	b := ballots[1]
	h := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID: types.NodeID{
				Key:          signer.PublicKey().String(),
				VRFPublicKey: vrfPubkey,
			},
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: ballotUnit,
	}
	h.SetID(&b.AtxID)
	tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
	b.EligibilityProof.Sig = b.EligibilityProof.Sig[1:]
	eligible, err := tv.CheckEligibility(context.TODO(), b)
	assert.ErrorIs(t, err, errIncorrectVRFSig)
	assert.False(t, eligible)
}

func TestCheckEligibility(t *testing.T) {
	tv := createTestValidator(t)
	signer := signing.NewEdSigner()
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(signer.PublicKey().Bytes())
	require.NoError(t, err)
	beacon := genBeacon()
	ballots := createBallots(t, signer, vrfSigner, genActiveSet(), beacon)
	rb := ballots[0]
	for _, id := range rb.EpochData.ActiveSet {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultUnit,
		}
		h.SetID(&id)
		tv.mdb.EXPECT().GetAtxHeader(id).Return(h, nil).Times(int(eligibleSlots))
	}
	tv.mm.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(int(eligibleSlots - 1))
	for _, b := range ballots {
		h := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID: types.NodeID{
					Key:          signer.PublicKey().String(),
					VRFPublicKey: vrfPubkey,
				},
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: ballotUnit,
		}
		h.SetID(&b.AtxID)
		tv.mdb.EXPECT().GetAtxHeader(b.AtxID).Return(h, nil).Times(1)
		tv.mbc.EXPECT().ReportBeaconFromBallot(epoch, b.ID(), types.BytesToBeacon(beacon), h.GetWeight()).Times(1)
		eligible, err := tv.CheckEligibility(context.TODO(), b)
		assert.NoError(t, err)
		assert.True(t, eligible)
	}
}
