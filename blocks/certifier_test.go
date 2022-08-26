package blocks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	hmocks "github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmock "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const defaultCnt = uint16(2)

type testCertifier struct {
	*Certifier
	db      *sql.Database
	nid     types.NodeID
	mOracle *hmocks.MockRolacle
	mPub    *pubsubmock.MockPublisher
	mClk    *mocks.MocklayerClock
	mb      *smocks.MockBeaconGetter
}

func newTestCertifier(t *testing.T) *testCertifier {
	t.Helper()
	types.SetLayersPerEpoch(3)
	db := sql.InMemory()
	signer := signing.NewEdSigner()
	nid := types.BytesToNodeID(signer.PublicKey().Bytes())
	ctrl := gomock.NewController(t)
	mo := hmocks.NewMockRolacle(ctrl)
	mp := pubsubmock.NewMockPublisher(ctrl)
	mc := mocks.NewMocklayerClock(ctrl)
	mb := smocks.NewMockBeaconGetter(ctrl)
	c := NewCertifier(db, mo, nid, signer, mp, mc, mb, WithCertifierLogger(logtest.New(t)))
	return &testCertifier{
		Certifier: c,
		db:        db,
		nid:       nid,
		mOracle:   mo,
		mPub:      mp,
		mClk:      mc,
		mb:        mb,
	}
}

func generateBlock(t *testing.T, db *sql.Database) *types.Block {
	t.Helper()
	block := types.NewExistingBlock(
		types.RandomBlockID(),
		types.InnerBlock{LayerIndex: types.NewLayerID(11)},
	)
	require.NoError(t, blocks.Add(db, block))
	return block
}

func genCertifyMsg(t *testing.T, lid types.LayerID, bid types.BlockID, cnt uint16) (types.NodeID, *types.CertifyMessage) {
	t.Helper()
	signer := signing.NewEdSigner()
	msg := &types.CertifyMessage{
		CertifyContent: types.CertifyContent{
			LayerID:        lid,
			BlockID:        bid,
			EligibilityCnt: cnt,
			Proof:          []byte("not a fraud"),
		},
	}
	msg.Signature = signer.Sign(msg.Bytes())
	return types.BytesToNodeID(signer.PublicKey().Bytes()), msg
}

func genEncodedMsg(t *testing.T, lid types.LayerID, bid types.BlockID) (types.NodeID, *types.CertifyMessage, []byte) {
	t.Helper()
	nid, msg := genCertifyMsg(t, lid, bid, defaultCnt)
	data, err := codec.Encode(msg)
	require.NoError(t, err)
	return nid, msg, data
}

func verifiedSaved(t *testing.T, db *sql.Database, lid types.LayerID, bid types.BlockID, expectedSigs int) {
	t.Helper()
	cert, err := layers.GetCert(db, lid)
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.Equal(t, bid, cert.BlockID)
	require.Len(t, cert.Signatures, expectedSigs)
}

func TestStartStop(t *testing.T) {
	tc := newTestCertifier(t)
	lid := types.NewLayerID(11)
	ch := make(chan struct{}, 1)
	tc.mClk.EXPECT().GetCurrentLayer().Return(lid).AnyTimes()
	tc.mClk.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(
		func(_ types.LayerID) chan struct{} {
			return ch
		}).AnyTimes()
	tc.Start()
	ch <- struct{}{}
	tc.Start() // calling Start() for the second time have no effect
	tc.Stop()
}

func Test_HandleSyncedCertificate(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CertifyThreshold / int(defaultCnt)
	b := generateBlock(t, tc.db)
	require.NoError(t, tc.RegisterDeadline(context.TODO(), b.LayerIndex, b.ID(), time.Now()))
	sigs := make([]types.CertifyMessage, numMsgs)
	for i := 0; i < numMsgs; i++ {
		nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		sigs[i] = *msg
	}
	cert := &types.Certificate{
		BlockID:    b.ID(),
		Signatures: sigs,
	}
	require.NoError(t, tc.HandleSyncedCertificate(context.TODO(), b.LayerIndex, cert))
	verifiedSaved(t, tc.db, b.LayerIndex, b.ID(), numMsgs)
}

func Test_HandleSyncedCertificate_NotEnoughEligibility(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CertifyThreshold/int(defaultCnt) - 1
	b := generateBlock(t, tc.db)
	require.NoError(t, tc.RegisterDeadline(context.TODO(), b.LayerIndex, b.ID(), time.Now()))
	sigs := make([]types.CertifyMessage, numMsgs)
	for i := 0; i < numMsgs; i++ {
		nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		sigs[i] = *msg
	}
	cert := &types.Certificate{
		BlockID:    b.ID(),
		Signatures: sigs,
	}
	require.ErrorIs(t, tc.HandleSyncedCertificate(context.TODO(), b.LayerIndex, cert), errInvalidCert)
}

func Test_HandleCertifyMessage(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterDeadline(context.TODO(), b.LayerIndex, b.ID(), time.Now()))
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
		Return(true, nil)
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationAccept, res)
}

func waitTillCertified(t *testing.T, tc *testCertifier, current types.LayerID, b *types.Block, numMsgs int) {
	ch := make(chan struct{}, 1)
	started := make(chan struct{}, 1)
	tc.mClk.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	tc.mClk.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(
		func(got types.LayerID) chan struct{} {
			if got == current {
				close(started)
			}
			return ch
		}).AnyTimes()
	tc.Start()
	<-started
	close(ch)
	var cert *types.Certificate
	require.Eventually(t, func() bool {
		cert, _ = layers.GetCert(tc.db, b.LayerIndex)
		return cert != nil
	}, 1*time.Second, 1*time.Millisecond)
	tc.Stop()
	require.Equal(t, b.ID(), cert.BlockID)
	require.Len(t, cert.Signatures, numMsgs)
}

func Test_HandleCertifyMessage_Certified(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CertifyThreshold/int(defaultCnt) + 2
	b := generateBlock(t, tc.db)
	require.NoError(t, tc.RegisterDeadline(context.TODO(), b.LayerIndex, b.ID(), time.Time{}))
	for i := 0; i < numMsgs; i++ {
		nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
		tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
		require.Equal(t, pubsub.ValidationAccept, res)
	}
	waitTillCertified(t, tc, b.LayerIndex, b, numMsgs)
}

func Test_HandleCertifyMessage_CertifiedWithEarlyMsgs(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CertifyThreshold/int(defaultCnt) + 2
	b := generateBlock(t, tc.db)
	current := b.LayerIndex.Sub(1)
	tc.mClk.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	for i := 0; i < numMsgs; i++ {
		nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
		tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
		require.Equal(t, pubsub.ValidationAccept, res)
	}
	require.NoError(t, tc.RegisterDeadline(context.TODO(), b.LayerIndex, b.ID(), time.Time{}))
	waitTillCertified(t, tc, current, b, numMsgs)
}

func Test_HandleCertifyMessage_TooManyEarlyMsgs(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CommitteeSize + 1
	b := generateBlock(t, tc.db)
	current := b.LayerIndex.Sub(1)
	tc.mClk.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	for i := 0; i < numMsgs; i++ {
		nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
		if i < tc.cfg.CommitteeSize {
			tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
				Return(true, nil)
		}
		res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
		if i < tc.cfg.CommitteeSize {
			require.Equal(t, pubsub.ValidationAccept, res)
		} else {
			// one too many
			require.Equal(t, pubsub.ValidationIgnore, res)
		}
	}
}

func Test_HandleCertifyMessage_MsgsTooEarly(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CommitteeSize
	b := generateBlock(t, tc.db)
	current := b.LayerIndex.Sub(tc.cfg.NumLayersToKeep)
	tc.mClk.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	for i := 0; i < numMsgs; i++ {
		_, _, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
		res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
		require.Equal(t, pubsub.ValidationIgnore, res)
	}
}

func Test_HandleCertifyMessage_Stopped(t *testing.T) {
	tc := newTestCertifier(t)
	tc.Stop()
	_, _, encoded := genEncodedMsg(t, types.NewLayerID(11), types.RandomBlockID())

	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_HandleCertifyMessage_CorruptedMsg(t *testing.T) {
	tc := newTestCertifier(t)
	_, _, encoded := genEncodedMsg(t, types.NewLayerID(11), types.BlockID{1, 2, 3})
	encoded = encoded[:1]

	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationReject, res)
}

func Test_HandleCertifyMessage_LayerNotRegistered(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	_, _, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mClk.EXPECT().GetCurrentLayer().Return(b.LayerIndex.Add(1)).AnyTimes()
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_HandleCertifyMessage_BlockNotRegistered(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	_, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterDeadline(context.TODO(), msg.LayerID, types.RandomBlockID(), time.Now()))
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_HandleCertifyMessage_BeaconNotAvailable(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	_, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterDeadline(context.TODO(), msg.LayerID, types.RandomBlockID(), time.Now()))
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.EmptyBeacon, errors.New("meh"))
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_OldLayersPruned(t *testing.T) {
	tc := newTestCertifier(t)
	lid := types.NewLayerID(11)

	require.NoError(t, tc.RegisterDeadline(context.TODO(), lid, types.RandomBlockID(), time.Now()))
	require.NoError(t, tc.RegisterDeadline(context.TODO(), lid.Add(1), types.RandomBlockID(), time.Now()))
	require.Equal(t, 2, tc.NumCached())

	current := lid.Add(tc.cfg.NumLayersToKeep + 1)
	ch := make(chan struct{}, 1)
	pruned := make(chan struct{}, 1)
	tc.mClk.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	tc.mClk.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(
		func(got types.LayerID) chan struct{} {
			if got == current.Add(1) {
				close(pruned)
			}
			return ch
		}).AnyTimes()
	tc.Start()
	ch <- struct{}{} // for current
	ch <- struct{}{} // for current+1
	<-pruned
	require.Equal(t, 1, tc.NumCached())
	tc.Stop()
}

func Test_CertifyIfEligible(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	proof := []byte("not a fraud")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(proof, nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, tc.nodeID, proof).Return(defaultCnt, nil)
	tc.mPub.EXPECT().Publish(gomock.Any(), pubsub.BlockCertify, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var msg types.CertifyMessage
			require.NoError(t, codec.Decode(got, &msg))
			pubkey, err := ed25519.ExtractPublicKey(msg.Bytes(), msg.Signature)
			require.NoError(t, err)
			require.Equal(t, tc.nodeID, types.BytesToNodeID(pubkey))
			require.Equal(t, b.LayerIndex, msg.LayerID)
			require.Equal(t, b.ID(), msg.BlockID)
			require.Equal(t, proof, msg.Proof)
			require.Equal(t, defaultCnt, msg.EligibilityCnt)
			return nil
		})
	require.NoError(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()))
}

func Test_CertifyIfEligible_NotEligible(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	proof := []byte("not a fraud")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(proof, nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, tc.nodeID, proof).Return(uint16(0), nil)
	require.NoError(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()))
}

func Test_CertifyIfEligible_EligibilityErr(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	errUnknown := errors.New("unknown")
	proof := []byte("not a fraud")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(proof, nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, tc.nodeID, proof).Return(uint16(0), errUnknown)
	require.ErrorIs(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()), errUnknown)
}

func Test_CertifyIfEligible_ProofErr(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	errUnknown := errors.New("unknown")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(nil, errUnknown)
	require.ErrorIs(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()), errUnknown)
}

func Test_CertifyIfEligible_BeaconNotAvailable(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.EmptyBeacon, errors.New("meh"))
	require.ErrorIs(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()), errBeaconNotAvailable)
}
