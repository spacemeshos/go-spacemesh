package proposals

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
)

func TestComputeWeightPerEligibility(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	ctrl := gomock.NewController(t)
	mbp := mocks.NewMockballotDB(ctrl)
	mAtxDb := mocks.NewMockatxDB(ctrl)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	rb := ballots[0]
	mbp.EXPECT().GetBallot(rb.ID()).Return(rb, nil).Times(len(ballots) - 1)
	for _, id := range rb.EpochData.ActiveSet {
		hdr := &types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
				StartTick:  0,
				EndTick:    1,
			},
			NumUnits: defaultATXUnit,
		}
		if id == rb.AtxID {
			hdr.NodeID = types.BytesToNodeID(signer.PublicKey().Bytes())
			hdr.NumUnits = testedATXUnit
		}
		hdr.SetID(&id)
		mAtxDb.EXPECT().GetAtxHeader(id).Return(hdr, nil).Times(len(ballots))
	}
	expectedWeight := util.WeightFromUint64(uint64(testedATXUnit)).Div(util.WeightFromUint64(uint64(eligibleSlots)))
	for _, b := range ballots {
		got, err := ComputeWeightPerEligibility(mAtxDb, mbp, b, layerAvgSize, layersPerEpoch)
		require.NoError(t, err)
		require.False(t, got.IsNil())
		require.Equal(t, 0, got.Cmp(expectedWeight))
	}
}

func TestComputeWeightPerEligibility_EmptyRefBallotID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	ctrl := gomock.NewController(t)
	mbp := mocks.NewMockballotDB(ctrl)
	mAtxDb := mocks.NewMockatxDB(ctrl)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(ballots))
	b := ballots[1]
	b.RefBallot = types.EmptyBallotID
	got, err := ComputeWeightPerEligibility(mAtxDb, mbp, b, layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, errBadBallotData)
	require.True(t, got.IsNil())
}

func TestComputeWeightPerEligibility_FailToGetRefBallot(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	ctrl := gomock.NewController(t)
	mbp := mocks.NewMockballotDB(ctrl)
	mAtxDb := mocks.NewMockatxDB(ctrl)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(ballots))
	rb := ballots[0]
	errUnknown := errors.New("unknown")
	mbp.EXPECT().GetBallot(rb.ID()).Return(nil, errUnknown)
	got, err := ComputeWeightPerEligibility(mAtxDb, mbp, ballots[1], layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, errUnknown)
	require.True(t, got.IsNil())
}

func TestComputeWeightPerEligibility_FailATX(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	ctrl := gomock.NewController(t)
	mbp := mocks.NewMockballotDB(ctrl)
	mAtxDb := mocks.NewMockatxDB(ctrl)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	ballots := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	errUnknown := errors.New("unknown")
	mAtxDb.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown)
	got, err := ComputeWeightPerEligibility(mAtxDb, mbp, ballots[0], layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, errUnknown)
	require.True(t, got.IsNil())
}
