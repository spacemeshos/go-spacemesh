package proposals

import (
	"math/big"
	"math/rand"
	"sort"
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
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

const (
	defaultATXUnit = uint32(5)
	testedATXUnit  = uint32(2)
	// eligibleSlots is calculated based on layerAvgSize, layersPerEpoch, epoch ATX weight and smesher's own weight.
	eligibleSlots = uint32(3)
	epoch         = types.EpochID(3)
)

func genActiveSet() types.ATXIDList {
	return types.ATXIDList{types.RandomATXID(), types.RandomATXID(), types.RandomATXID(), types.RandomATXID()}
}

func createBallots(tb testing.TB, signer *signing.EdSigner, activeSet types.ATXIDList, beacon types.Beacon) []*types.Ballot {
	totalWeight := uint64(len(activeSet)-1)*uint64(defaultATXUnit) + uint64(testedATXUnit)
	slots, err := GetNumEligibleSlots(uint64(testedATXUnit), 0, totalWeight, layerAvgSize, layersPerEpoch)
	require.NoError(tb, err)
	require.Equal(tb, eligibleSlots, slots)
	eligibilityProofs := map[types.LayerID][]types.VotingEligibility{}
	order := make([]types.LayerID, 0, eligibleSlots)

	nonce := types.VRFPostIndex(1)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(tb, err)

	for counter := uint32(0); counter < eligibleSlots; counter++ {
		message, err := SerializeVRFMessage(beacon, epoch, nonce, counter)
		require.NoError(tb, err)
		vrfSig := vrfSigner.Sign(message)
		eligibleLayer := CalcEligibleLayer(epoch, layersPerEpoch, vrfSig)
		if _, exist := eligibilityProofs[eligibleLayer]; !exist {
			order = append(order, eligibleLayer)
		}
		eligibilityProofs[eligibleLayer] = append(eligibilityProofs[eligibleLayer], types.VotingEligibility{
			J:   counter,
			Sig: vrfSig,
		})
	}
	sort.Slice(order, func(i, j int) bool { return order[i].Before(order[j]) })
	blts := make([]*types.Ballot, 0, eligibleSlots)
	for _, lyr := range order {
		proofs := eligibilityProofs[lyr]
		isRef := len(blts) == 0
		b := types.RandomBallot()
		b.AtxID = activeSet[0]
		b.Layer = lyr
		if isRef {
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{
				ActiveSetHash:    activeSet.Hash(),
				Beacon:           beacon,
				EligibilityCount: eligibleSlots,
			}
		} else {
			b.RefBallot = blts[0].ID()
		}
		b.EligibilityProofs = proofs
		b.Signature = signer.Sign(signing.BALLOT, b.SignedBytes())
		b.SmesherID = signer.NodeID()
		require.NoError(tb, b.Initialize())
		blts = append(blts, b)
	}
	return blts
}

func TestComputeWeightPerEligibility(t *testing.T) {
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	beacon := types.Beacon{1, 1, 1}
	actives := genActiveSet()
	blts := createBallots(t, signer, actives, beacon)
	rb := blts[0]
	activesets.Add(cdb, rb.EpochData.ActiveSetHash, &types.EpochActiveSet{Set: actives})
	require.NoError(t, ballots.Add(cdb, rb))
	for _, id := range actives {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: epoch - 1,
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
		got, err := ComputeWeightPerEligibility(cdb, b)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, 0, got.Cmp(expectedWeight))
	}
}

func TestComputeWeightPerEligibility_EmptyRefBallotID(t *testing.T) {
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
	got, err := ComputeWeightPerEligibility(cdb, b)
	require.ErrorIs(t, err, putil.ErrBadBallotData)
	require.Nil(t, got)
}

func TestComputeWeightPerEligibility_FailToGetRefBallot(t *testing.T) {
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	beacon := types.Beacon{1, 1, 1}
	blts := createBallots(t, signer, genActiveSet(), beacon)
	require.GreaterOrEqual(t, 2, len(blts))
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	got, err := ComputeWeightPerEligibility(cdb, blts[1])
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "missing ref ballot"))
	require.Nil(t, got)
}

func TestComputeWeightPerEligibility_FailATX(t *testing.T) {
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)
	beacon := types.Beacon{1, 1, 1}
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	actives := genActiveSet()
	blts := createBallots(t, signer, actives, beacon)
	rb := blts[0]
	activesets.Add(cdb, rb.EpochData.ActiveSetHash, &types.EpochActiveSet{Set: actives})
	got, err := ComputeWeightPerEligibility(cdb, rb)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "missing atx"))
	require.Nil(t, got)
}
