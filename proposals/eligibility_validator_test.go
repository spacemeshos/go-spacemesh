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

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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

func genActiveSetAndSave(t *testing.T, cdb *datastore.CachedDB, signer *signing.EdSigner) types.ATXIDList {
	t.Helper()
	activeset := types.ATXIDList{types.RandomATXID(), types.RandomATXID(), types.RandomATXID(), types.RandomATXID()}

	nonce := types.VRFPostIndex(1)
	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: epoch - 1,
		},
		NumUnits: testedATXUnit,
		VRFNonce: &nonce,
	}}
	atx.SetID(activeset[0])
	atx.SetEffectiveNumUnits(testedATXUnit)
	atx.SetReceived(time.Now())
	activation.SignAndFinalizeAtx(signer, atx)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(cdb, vAtx))

	for _, id := range activeset[1:] {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: epoch - 1,
			},
			NumUnits: defaultATXUnit,
		}}
		atx.SetID(id)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(cdb, vAtx))
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
		Validator: NewEligibilityValidator(layerAvgSize, layersPerEpoch, datastore.NewCachedDB(sql.InMemory(), lg), ms.mbc, ms.mm, lg, ms.mvrf,
			WithNonceFetcher(ms.mNonce),
		),
		mockSet: ms,
	}
}

func createBallots(tb testing.TB, signer *signing.EdSigner, activeSet types.ATXIDList, beacon types.Beacon) []*types.Ballot {
	totalWeight := uint64(len(activeSet)-1)*uint64(defaultATXUnit) + uint64(testedATXUnit)
	slots, err := GetNumEligibleSlots(uint64(testedATXUnit), totalWeight, layerAvgSize, layersPerEpoch)
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
			b.ActiveSet = activeSet
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

func TestCheckEligibility_FailedToGetRefBallot(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1128))),
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
	rb.ActiveSet = nil
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

	activeset := genActiveSetAndSave(t, tv.cdb, signer)

	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	randomAtx := types.RandomATXID()
	rb.ActiveSet = append(rb.ActiveSet, randomAtx)
	rb.AtxID = randomAtx
	eligible, err := tv.CheckEligibility(context.Background(), rb)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.ErrorContains(t, err, "get ATX header")
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
			PublishEpoch: epoch,
		},
		NumUnits: testedATXUnit,
	}}
	atx.SetID(rb.ActiveSet[0])
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tv.cdb, vAtx))

	for _, id := range rb.ActiveSet[1:] {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: epoch - 1,
			},
			NumUnits: defaultATXUnit,
		}}
		atx.SetID(id)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(tv.cdb, vAtx))
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
	signer2, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1002))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer2)
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	eligible, err := tv.CheckEligibility(context.Background(), blts[1])
	require.ErrorIs(t, err, errPublicKeyMismatch)
	require.False(t, eligible)
}

func TestCheckEligibility_IncorrectEligibilityCount(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer)
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	rb.EpochData.EligibilityCount = eligibleSlots - 1

	got, err := tv.CheckEligibility(context.Background(), rb)
	require.ErrorIs(t, err, errIncorrectEligCount)
	require.False(t, got)
}

func TestCheckEligibility_BadCounter(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1001))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer)
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	b := blts[1]
	b.EligibilityProofs[0].J = b.EligibilityProofs[0].J + 100

	tv.mNonce.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).Times(1)
	eligible, err := tv.CheckEligibility(context.Background(), b)
	require.ErrorIs(t, err, errIncorrectCounter)
	require.False(t, eligible)
}

func TestCheckEligibility_InvalidOrder(t *testing.T) {
	tv := createTestValidator(t)
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rand.New(rand.NewSource(1003))),
	)
	require.NoError(t, err)

	activeset := genActiveSetAndSave(t, tv.cdb, signer)
	blts := createBallots(t, signer, activeset, types.Beacon{10})
	rb := blts[0]
	require.Len(t, rb.EligibilityProofs, 2)
	rb.EligibilityProofs[0], rb.EligibilityProofs[1] = rb.EligibilityProofs[1], rb.EligibilityProofs[0]

	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	tv.mNonce.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).Times(1)

	eligible, err := tv.CheckEligibility(context.Background(), rb)
	require.ErrorIs(t, err, errInvalidProofsOrder)
	require.False(t, eligible)

	rb.EligibilityProofs[0], rb.EligibilityProofs[1] = rb.EligibilityProofs[1], rb.EligibilityProofs[0]
	rb.EligibilityProofs = append(rb.EligibilityProofs, types.VotingEligibility{J: 2})

	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	tv.mNonce.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).Times(1)

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

	activeset := genActiveSetAndSave(t, tv.cdb, signer)
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))

	b := blts[1]
	b.EligibilityProofs[0].Sig = types.RandomVrfSignature()
	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), b.EligibilityProofs[0].Sig).Return(false)
	tv.mNonce.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).Times(1)

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

	activeset := genActiveSetAndSave(t, tv.cdb, signer)
	blts := createBallots(t, signer, activeset, types.Beacon{1, 1, 1})
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))

	b := blts[1]
	b.EligibilityProofs[0].Sig = types.RandomVrfSignature()
	tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), b.EligibilityProofs[0].Sig).Return(false)
	tv.mNonce.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).Times(1)

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
	activeset := genActiveSetAndSave(t, tv.cdb, signer)
	blts := createBallots(t, signer, activeset, beacon)
	rb := blts[0]
	require.NoError(t, ballots.Add(tv.cdb, rb))
	for _, b := range blts {
		hdr, err := tv.cdb.GetAtxHeader(b.AtxID)
		require.NoError(t, err)
		weightPer := fixed.DivUint64(hdr.GetWeight(), uint64(eligibleSlots))
		tv.mbc.EXPECT().ReportBeaconFromBallot(epoch, b, beacon, weightPer).Times(1)
		tv.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
		tv.mNonce.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).Times(1)
		got, err := tv.CheckEligibility(context.Background(), b)
		require.NoError(t, err)
		require.True(t, got)
	}
}

func TestCheckEligibility_AtxIdMismatch(t *testing.T) {
	tv := createTestValidator(t)
	refballot := types.Ballot{}
	refballot.SetID(types.BallotID{1})
	refballot.AtxID = types.ATXID{1}
	refballot.EpochData = &types.EpochData{}
	require.NoError(t, ballots.Add(tv.cdb, &refballot))

	ballot := &types.Ballot{}
	ballot.EligibilityProofs = []types.VotingEligibility{{}}
	ballot.RefBallot = refballot.ID()
	ballot.AtxID = types.ATXID{2}

	eligibile, err := tv.CheckEligibility(context.Background(), ballot)
	require.ErrorContains(t, err, "should be sharing atx")
	require.False(t, eligibile)
}

func TestCheckEligibility_AtxNotIncluded(t *testing.T) {
	tv := createTestValidator(t)

	atx1 := &types.VerifiedActivationTx{ActivationTx: &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NumUnits: 2,
		},
	}}
	atx1.SetID(types.ATXID{1})
	atx1.SetEffectiveNumUnits(atx1.NumUnits)
	atx2 := &types.VerifiedActivationTx{ActivationTx: &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NumUnits: 2,
		},
	}}
	atx2.SetID(types.ATXID{2})
	atx2.SetEffectiveNumUnits(atx2.NumUnits)
	require.NoError(t, atxs.Add(tv.cdb, atx1))
	require.NoError(t, atxs.Add(tv.cdb, atx2))

	ballot := &types.Ballot{}
	ballot.EligibilityProofs = []types.VotingEligibility{{}}
	ballot.SetID(types.BallotID{1})
	ballot.AtxID = types.ATXID{3}
	activeSet := types.ATXIDList{atx1.ID(), atx2.ID()}
	ballot.EpochData = &types.EpochData{
		ActiveSetHash: activeSet.Hash(),
		Beacon:        types.Beacon{1},
	}
	ballot.ActiveSet = activeSet
	require.NoError(t, ballots.Add(tv.cdb, ballot))

	eligibile, err := tv.CheckEligibility(context.Background(), ballot)
	require.ErrorContains(t, err, "is not included into the active set")
	require.False(t, eligibile)
}
