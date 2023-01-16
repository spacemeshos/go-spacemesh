package proposals

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals/util"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
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

func genActiveSetAndSave(t *testing.T, cdb *datastore.CachedDB, nid types.NodeID) types.ATXIDList {
	t.Helper()
	activeset := types.ATXIDList{types.RandomATXID(), types.RandomATXID(), types.RandomATXID(), types.RandomATXID()}

	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
		},
		NumUnits: testedATXUnit,
	}}
	atx.SetID(&activeset[0])
	atx.SetNodeID(&nid)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(cdb, vAtx, time.Now()))

	for _, id := range activeset[1:] {
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			},
			NumUnits: defaultATXUnit,
		}}
		atx.SetID(&id)
		atx.SetNodeID(&nodeID)
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(cdb, vAtx, time.Now()))
	}
	return activeset
}

type testValidator struct {
	*Validator
	*mockSet
}

func createTestValidator(tb testing.TB) *testValidator {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(tb)
	lg := logtest.New(tb)

	return &testValidator{
		Validator: NewEligibilityValidator(layerAvgSize, layersPerEpoch, datastore.NewCachedDB(sql.InMemory(), lg), ms.mbc, ms.mm, lg, ms.mvrf),
		mockSet:   ms,
	}
}

func createBallots(tb testing.TB, signer *signing.EdSigner, activeSet types.ATXIDList, beacon types.Beacon) []*types.Ballot {
	totalWeight := uint64(len(activeSet)-1)*uint64(defaultATXUnit) + uint64(testedATXUnit)
	slots, err := GetNumEligibleSlots(uint64(testedATXUnit), totalWeight, layerAvgSize, layersPerEpoch)
	require.NoError(tb, err)
	require.Equal(tb, eligibleSlots, slots)
	eligibilityProofs := map[types.LayerID][]types.VotingEligibility{}
	order := make([]types.LayerID, 0, eligibleSlots)

	vrfSigner, err := signer.VRFSigner(signing.WithNonceForNode(1, signer.NodeID()))
	require.NoError(tb, err)

	for counter := uint32(0); counter < eligibleSlots; counter++ {
		message, err := SerializeVRFMessage(beacon, epoch, counter)
		require.NoError(tb, err)
		vrfSig, err := vrfSigner.Sign(message, epoch)
		require.NoError(tb, err)
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
				ActiveSet: activeSet,
				Beacon:    beacon,
			}
		} else {
			b.RefBallot = blts[0].ID()
		}
		b.EligibilityProofs = proofs
		b.Signature = signer.Sign(b.SignedBytes())
		require.NoError(tb, b.Initialize())
		blts = append(blts, b)
	}
	return blts
}

func TestCheckEligibility_FailedToGetRefBallot(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "get ref ballot"))
	require.False(t, eligible)
}

func TestCheckEligibility_RefBallotMissingEpochData(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	rb := blts[0]
	rb.EpochData = nil
	require.NoError(t, ballots.Add(tv.cdb, rb))
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, errMissingEpochData)
	require.False(t, eligible)
}

func TestCheckEligibility_RefBallotMissingBeacon(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	rb := blts[0]
	rb.EpochData.Beacon = types.EmptyBeacon
	require.NoError(t, ballots.Add(tv.cdb, rb))
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, errMissingBeacon)
	require.False(t, eligible)
}

func TestCheckEligibility_RefBallotEmptyActiveSet(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	rb := blts[0]
	rb.EpochData.ActiveSet = nil
	require.NoError(t, ballots.Add(tv.cdb, rb))
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, errEmptyActiveSet)
	require.False(t, eligible)
}

func TestCheckEligibility_FailToGetActiveSetATXHeader(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "get ATX header"))
	require.False(t, eligible)
}

func TestCheckEligibility_FailToGetBallotATXHeader(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer.NodeID())
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	b := blts[1]
	b.AtxID = types.RandomATXID()
	eligible, err := tv.CheckEligibility(context.Background(), b)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.True(t, strings.Contains(err.Error(), "get ballot ATX header"))
	require.False(t, eligible)
}

func TestCheckEligibility_TargetEpochMismatch(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))

	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: epoch.FirstLayer(),
		},
		NumUnits: testedATXUnit,
	}}
	atx.SetID(&rb.EpochData.ActiveSet[0])
	nodeID := signer.NodeID()
	atx.SetNodeID(&nodeID)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tv.cdb, vAtx, time.Now()))

	for _, id := range rb.EpochData.ActiveSet[1:] {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			},
			NumUnits: defaultATXUnit,
		}}
		atx.SetID(&id)
		atx.SetNodeID(&types.NodeID{})
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(tv.cdb, vAtx, time.Now()))
	}
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, errTargetEpochMismatch)
	require.False(t, eligible)
}

func TestCheckEligibility_KeyMismatch(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, types.NodeID{1})
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, errPublicKeyMismatch)
	require.False(t, eligible)
}

func TestCheckEligibility_ZeroTotalWeight(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	blts := createBallots(t, signer, genActiveSet(), types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))

	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
		},
		NumUnits: 0,
	}}
	atx.SetID(&rb.EpochData.ActiveSet[0])
	nodeID := signer.NodeID()
	atx.SetNodeID(&nodeID)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tv.cdb, vAtx, time.Now()))

	for _, id := range rb.EpochData.ActiveSet[1:] {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer().Sub(layersPerEpoch),
			},
			NumUnits: 0,
		}}
		atx.SetID(&id)
		atx.SetNodeID(&types.NodeID{})
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(tv.cdb, vAtx, time.Now()))
	}
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, util.ErrZeroTotalWeight)
	require.False(t, eligible)
}

func TestCheckEligibility_BadCounter(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer.NodeID())
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	b := blts[1]
	b.EligibilityProofs[0].J = b.EligibilityProofs[0].J + 100
	eligible, err := tv.CheckEligibility(context.Background(), b)
	require.ErrorIs(t, err, errIncorrectCounter)
	require.False(t, eligible)
}

func TestCheckEligibility_InvalidOrder(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1121))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer.NodeID())
	blts := createBallots(t, signer, activeset, types.Beacon{10})
	rb := blts[0]
	require.Len(t, rb.EligibilityProofs, 2)
	rb.EligibilityProofs[0], rb.EligibilityProofs[1] = rb.EligibilityProofs[1], rb.EligibilityProofs[0]

	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	eligible, err := tv.CheckEligibility(context.Background(), rb)
	require.ErrorIs(t, err, errInvalidProofsOrder)
	require.False(t, eligible)

	rb.EligibilityProofs[0], rb.EligibilityProofs[1] = rb.EligibilityProofs[1], rb.EligibilityProofs[0]
	rb.EligibilityProofs = append(rb.EligibilityProofs, types.VotingEligibility{J: 2})
	eligible, err = tv.CheckEligibility(context.Background(), rb)
	require.ErrorIs(t, err, errInvalidProofsOrder)
	require.False(t, eligible)
}

func TestCheckEligibility_BadVRFSignature(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer.NodeID())
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))

	b := blts[1]
	b.EligibilityProofs[0].Sig = b.EligibilityProofs[0].Sig[1:]
	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), b.EligibilityProofs[0].Sig).Return(false)

	eligible, err := tv.CheckEligibility(context.Background(), b)
	require.ErrorIs(t, err, errIncorrectVRFSig)
	require.False(t, eligible)
}

func TestCheckEligibility_IncorrectLayerIndex(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer.NodeID())
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))

	b := blts[1]
	b.EligibilityProofs[0].Sig = b.EligibilityProofs[0].Sig[1:]
	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), b.EligibilityProofs[0].Sig).Return(false)

	eligible, err := tv.CheckEligibility(context.Background(), b)
	require.ErrorIs(t, err, errIncorrectVRFSig)
	require.False(t, eligible)
}

func TestCheckEligibility_EmptyEligibilityList(t *testing.T) {
	tv := createTestValidator(t)
	eligibile, err := tv.CheckEligibility(context.Background(), &types.Ballot{})
	require.ErrorContains(t, err, "empty eligibility list")
	require.False(t, eligibile)
}

func TestCheckEligibility(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	beacon := types.Beacon{1, 1, 1}
	activeset := genActiveSetAndSave(t, tv.cdb, signer.NodeID())
	blts := createBallots(t, signer, activeset, beacon)
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	for _, b := range blts {
		hdr, err := tv.cdb.GetAtxHeader(b.AtxID)
		require.NoError(t, err)
		weightPer := fixed.DivUint64(hdr.GetWeight(), uint64(eligibleSlots))
		tv.mbc.EXPECT().ReportBeaconFromBallot(epoch, b, beacon, weightPer).Times(1)
		tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
		got, err := tv.CheckEligibility(context.Background(), b)
		require.NoError(t, err)
		require.True(t, got)
	}
}
