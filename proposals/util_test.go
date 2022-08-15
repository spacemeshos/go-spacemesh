package proposals

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

func TestComputeWeightPerEligibility(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	rb := blts[0]
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	require.NoError(t, ballots.Add(cdb, rb))
	for _, id := range rb.EpochData.ActiveSet {
		hdr := types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			},
			NumUnits: defaultATXUnit,
		}
		hdr.Verify(0, 1)
		if id == rb.AtxID {
			hdr.NodeID = types.BytesToNodeID(signer.PublicKey().Bytes())
			hdr.NumUnits = testedATXUnit
		}
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{ActivationTxHeader: hdr}}
		atx.SetID(&id)
		require.NoError(t, atxs.Add(cdb, atx, time.Now()))
	}
	expectedWeight := util.WeightFromUint64(uint64(testedATXUnit)).Div(util.WeightFromUint64(uint64(eligibleSlots)))
	for _, b := range blts {
		got, err := ComputeWeightPerEligibility(cdb, b, layerAvgSize, layersPerEpoch)
		require.NoError(t, err)
		require.False(t, got.IsNil())
		require.Equal(t, 0, got.Cmp(expectedWeight))
	}
}

func TestComputeWeightPerEligibility_EmptyRefBallotID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(blts))
	b := blts[1]
	b.RefBallot = types.EmptyBallotID
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, b, layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, errBadBallotData)
	require.True(t, got.IsNil())
}

func TestComputeWeightPerEligibility_FailToGetRefBallot(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(blts))
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, blts[1], layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "missing ref ballot"))
	require.True(t, got.IsNil())
}

func TestComputeWeightPerEligibility_FailATX(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer := genSigner()
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, signer.VRFSigner(), genActiveSet(), beacon)
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, blts[0], layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "missing atx"))
	require.True(t, got.IsNil())
}
