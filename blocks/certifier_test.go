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
)

const defaultCnt = uint16(2)

type testCertifier struct {
	*Certifier
	db      *sql.Database
	nid     types.NodeID
	mOracle *hmocks.MockRolacle
	mPub    *pubsubmock.MockPublisher
	mClk    *mocks.MocklayerClock
}

func newTestCertifier(t *testing.T) *testCertifier {
	t.Helper()
	db := sql.InMemory()
	signer := signing.NewEdSigner()
	nid := types.BytesToNodeID(signer.PublicKey().Bytes())
	ctrl := gomock.NewController(t)
	mo := hmocks.NewMockRolacle(ctrl)
	mp := pubsubmock.NewMockPublisher(ctrl)
	mc := mocks.NewMocklayerClock(ctrl)
	c := NewCertifier(db, mo, nid, signer, mp, mc, WithCertifierLogger(logtest.New(t)))
	return &testCertifier{
		Certifier: c,
		db:        db,
		nid:       nid,
		mOracle:   mo,
		mPub:      mp,
		mClk:      mc,
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

func Test_HandleCertifyMessage(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	tc.RegisterDeadline(b.LayerIndex, b.ID(), time.Now())
	tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
		Return(true, nil)
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationAccept, res)
}

func Test_HandleCertifyMessage_Certified(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CommitteeSize
	cutoff := tc.cfg.CertifyThreshold / int(defaultCnt)
	b := generateBlock(t, tc.db)
	tc.RegisterDeadline(b.LayerIndex, b.ID(), time.Now())
	for i := 0; i < numMsgs; i++ {
		nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		if i < cutoff {
			tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
				Return(true, nil)
		}
		res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
		require.Equal(t, pubsub.ValidationAccept, res)
	}
	verifiedSaved(t, tc.db, b.LayerIndex, b.ID(), cutoff)
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

	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_HandleCertifyMessage_BlockNotRegistered(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	_, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	tc.RegisterDeadline(msg.LayerID, types.RandomBlockID(), time.Now())
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_HandleCertifyMessage_TooLate(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	_, _, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	tc.RegisterDeadline(b.LayerIndex, b.ID(), time.Time{})
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_OldLayersPruned(t *testing.T) {
	tc := newTestCertifier(t)
	lid := types.NewLayerID(11)

	tc.RegisterDeadline(lid, types.RandomBlockID(), time.Now())
	tc.RegisterDeadline(lid.Add(1), types.RandomBlockID(), time.Now())
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
	proof := []byte("not a fraud")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(proof, nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, tc.nodeID, proof).Return(uint16(0), nil)
	require.NoError(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()))
}

func Test_CertifyIfEligible_EligibilityErr(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	errUnknown := errors.New("unknown")
	proof := []byte("not a fraud")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(proof, nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, tc.nodeID, proof).Return(uint16(0), errUnknown)
	require.ErrorIs(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()), errUnknown)
}

func Test_CertifyIfEligible_ProofErr(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	errUnknown := errors.New("unknown")
	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(nil, errUnknown)
	require.ErrorIs(t, tc.CertifyIfEligible(context.TODO(), tc.logger, b.LayerIndex, b.ID()), errUnknown)
}
