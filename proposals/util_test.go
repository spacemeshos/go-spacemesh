package proposals

import (
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	putil "github.com/spacemeshos/go-spacemesh/proposals/util"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

func TestComputeWeightPerEligibility(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, genActiveSet(), beacon)
	rb := blts[0]
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	require.NoError(t, ballots.Add(cdb, rb))
	for _, id := range rb.EpochData.ActiveSet {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			},
			NumUnits: defaultATXUnit,
		}}
		atx.SetID(id)
		if id == rb.AtxID {
			atx.NumUnits = testedATXUnit
		}
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(cdb, vAtx))
	}
	expectedWeight := big.NewRat(int64(testedATXUnit), int64(eligibleSlots))
	for _, b := range blts {
		got, err := ComputeWeightPerEligibility(cdb, b, layerAvgSize, layersPerEpoch)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, 0, got.Cmp(expectedWeight))
	}
}

func TestComputeWeightPerEligibility_EmptyRefBallotID(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(blts))
	b := blts[1]
	b.RefBallot = types.EmptyBallotID
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, b, layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, putil.ErrBadBallotData)
	require.Nil(t, got)
}

func TestComputeWeightPerEligibility_FailToGetRefBallot(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(blts))
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, blts[1], layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "missing ref ballot"))
	require.Nil(t, got)
}

func TestComputeWeightPerEligibility_FailATX(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, genActiveSet(), beacon)
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, blts[0], layerAvgSize, layersPerEpoch)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "missing atx"))
	require.Nil(t, got)
}
