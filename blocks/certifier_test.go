package blocks

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
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
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const defaultCnt = uint16(2)

type testCertifier struct {
	*Certifier
	db        *sql.Database
	nid       types.NodeID
	mOracle   *hmocks.MockRolacle
	mPub      *pubsubmock.MockPublisher
	mClk      *mocks.MocklayerClock
	mb        *smocks.MockBeaconGetter
	mtortoise *smocks.MockTortoise
}

func newTestCertifier(t *testing.T) *testCertifier {
	t.Helper()
	types.SetLayersPerEpoch(3)
	db := sql.InMemory()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	nid := signer.NodeID()
	ctrl := gomock.NewController(t)
	mo := hmocks.NewMockRolacle(ctrl)
	mp := pubsubmock.NewMockPublisher(ctrl)
	mc := mocks.NewMocklayerClock(ctrl)
	mb := smocks.NewMockBeaconGetter(ctrl)
	mtortoise := smocks.NewMockTortoise(ctrl)
	c := NewCertifier(db, mo, nid, signer, mp, mc, mb, mtortoise, WithCertifierLogger(logtest.New(t)))
	return &testCertifier{
		Certifier: c,
		db:        db,
		nid:       nid,
		mOracle:   mo,
		mPub:      mp,
		mClk:      mc,
		mb:        mb,
		mtortoise: mtortoise,
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

func genCertifyMsg(tb testing.TB, lid types.LayerID, bid types.BlockID, cnt uint16) (types.NodeID, *types.CertifyMessage) {
	tb.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	msg := &types.CertifyMessage{
		CertifyContent: types.CertifyContent{
			LayerID:        lid,
			BlockID:        bid,
			EligibilityCnt: cnt,
			Proof:          []byte("not a fraud"),
		},
	}
	msg.Signature = signer.Sign(msg.Bytes())
	return signer.NodeID(), msg
}

func genEncodedMsg(t *testing.T, lid types.LayerID, bid types.BlockID) (types.NodeID, *types.CertifyMessage, []byte) {
	t.Helper()
	nid, msg := genCertifyMsg(t, lid, bid, defaultCnt)
	data, err := codec.Encode(msg)
	require.NoError(t, err)
	return nid, msg, data
}

func verifyCerts(t *testing.T, db *sql.Database, lid types.LayerID, expected map[types.BlockID]bool) {
	t.Helper()
	var allValids []types.BlockID
	certs, err := certificates.Get(db, lid)
	if len(expected) == 0 {
		require.ErrorIs(t, err, sql.ErrNotFound)
	} else {
		require.NoError(t, err)
		require.Equal(t, len(expected), len(certs))
		for _, cert := range certs {
			ev, ok := expected[cert.Block]
			require.True(t, ok)
			require.Equal(t, ev, cert.Valid)
			delete(expected, cert.Block)
			if ev {
				allValids = append(allValids, cert.Block)
			}
		}
		require.Empty(t, expected)
	}
	// check hare output
	ho, err := certificates.GetHareOutput(db, lid)
	if len(allValids) == 0 {
		require.ErrorIs(t, err, sql.ErrNotFound)
	} else if len(allValids) == 1 {
		require.NoError(t, err)
		require.Equal(t, allValids[0], ho)
	} else {
		require.NoError(t, err)
		require.Equal(t, types.EmptyBlockID, ho)
	}
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
	tc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, b.ID())
	require.NoError(t, tc.HandleSyncedCertificate(context.TODO(), b.LayerIndex, cert))
	verifyCerts(t, tc.db, b.LayerIndex, map[types.BlockID]bool{b.ID(): true})
}

func Test_HandleSyncedCertificate_HareOutputTrumped(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CertifyThreshold / int(defaultCnt)
	b := generateBlock(t, tc.db)
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
	ho := types.RandomBlockID()
	require.NoError(t, certificates.SetHareOutput(tc.db, b.LayerIndex, ho))
	tc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, b.ID())
	require.NoError(t, tc.HandleSyncedCertificate(context.TODO(), b.LayerIndex, cert))
	verifyCerts(t, tc.db, b.LayerIndex, map[types.BlockID]bool{b.ID(): true, ho: false})
}

func Test_HandleSyncedCertificate_MultipleCertificates(t *testing.T) {
	tt := []struct {
		name           string
		sameBid, valid bool
		err            error
	}{
		{
			name:    "old cert has same block ID",
			sameBid: true,
		},
		{
			name: "old cert no longer valid",
		},
		{
			name:  "old cert still valid",
			valid: true,
			err:   errMultipleCerts,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tcc := newTestCertifier(t)
			numMsgs := tcc.cfg.CertifyThreshold / int(defaultCnt)
			b := generateBlock(t, tcc.db)
			bid := types.RandomBlockID()
			if tc.sameBid {
				bid = b.ID()
			}
			oldCert := &types.Certificate{
				BlockID: bid,
			}
			for i := 0; i < numMsgs; i++ {
				nid, msg, _ := genEncodedMsg(t, b.LayerIndex, bid)
				oldCert.Signatures = append(oldCert.Signatures, *msg)
				if !tc.sameBid {
					tcc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tcc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
						Return(tc.valid, nil)
				}
			}
			require.NoError(t, certificates.Add(tcc.db, b.LayerIndex, oldCert))

			sigs := make([]types.CertifyMessage, numMsgs)
			for i := 0; i < numMsgs; i++ {
				nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
				tcc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tcc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
					Return(true, nil)
				sigs[i] = *msg
			}
			cert := &types.Certificate{
				BlockID:    b.ID(),
				Signatures: sigs,
			}
			if tc.err != nil {
				tcc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, types.EmptyBlockID)
			} else {
				tcc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, b.ID())
			}
			require.ErrorIs(t, tcc.HandleSyncedCertificate(context.TODO(), b.LayerIndex, cert), tc.err)
			expected := map[types.BlockID]bool{}
			if tc.err == nil {
				if tc.sameBid {
					expected[b.ID()] = true
				} else {
					expected[b.ID()] = true
					expected[bid] = false
				}
			} else {
				expected[b.ID()] = true
				expected[bid] = true
			}
			verifyCerts(t, tcc.db, b.LayerIndex, expected)
		})
	}
}

func Test_HandleSyncedCertificate_NotEnoughEligibility(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CertifyThreshold/int(defaultCnt) - 1
	b := generateBlock(t, tc.db)
	require.NoError(t, tc.RegisterForCert(context.TODO(), b.LayerIndex, b.ID()))
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
	cfg := defaultCertConfig()
	tt := []struct {
		name     string
		diff     int
		expected pubsub.ValidationResult
	}{
		{
			name:     "on time",
			expected: pubsub.ValidationAccept,
		},
		{
			name:     "genesis",
			expected: pubsub.ValidationIgnore,
			diff:     -10,
		},
		{
			name:     "boundary - early",
			expected: pubsub.ValidationAccept,
			diff:     -1 * int(numEarlyLayers),
		},
		{
			name:     "boundary - late",
			expected: pubsub.ValidationAccept,
			diff:     int(cfg.WaitSigLayers + 1),
		},
		{
			name:     "too early",
			expected: pubsub.ValidationIgnore,
			diff:     -1 * int(numEarlyLayers+1),
		},
		{
			name:     "too late",
			expected: pubsub.ValidationIgnore,
			diff:     int(cfg.WaitSigLayers + 2),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			testCert := newTestCertifier(t)
			b := generateBlock(t, testCert.db)
			nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

			current := b.LayerIndex
			if tc.diff > 0 {
				current = current.Add(uint32(tc.diff))
			} else {
				current = current.Sub(uint32(-1 * tc.diff))
			}
			testCert.mClk.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
			testCert.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
			require.NoError(t, testCert.RegisterForCert(context.TODO(), b.LayerIndex, b.ID()))
			if tc.expected == pubsub.ValidationAccept {
				testCert.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, testCert.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
					Return(true, nil)
			}
			res := testCert.HandleCertifyMessage(context.TODO(), "peer", encoded)
			require.Equal(t, tc.expected, res)
		})
	}
}

func Test_HandleCertifyMessage_Certified(t *testing.T) {
	tt := []struct {
		name       string
		concurrent bool
	}{
		{
			name: "sequential",
		},
		{
			name:       "concurrent",
			concurrent: true,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tcc := newTestCertifier(t)
			numMsgs := tcc.cfg.CommitteeSize
			cutoff := tcc.cfg.CertifyThreshold / int(defaultCnt)
			b := generateBlock(t, tcc.db)
			ho := types.RandomBlockID()
			require.NoError(t, certificates.SetHareOutput(tcc.db, b.LayerIndex, ho))
			tcc.mClk.EXPECT().GetCurrentLayer().Return(b.LayerIndex).AnyTimes()
			tcc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil).AnyTimes()
			require.NoError(t, tcc.RegisterForCert(context.TODO(), b.LayerIndex, b.ID()))
			if tc.concurrent {
				tcc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tcc.cfg.CommitteeSize, gomock.Any(), gomock.Any(), defaultCnt).
					Return(true, nil).MinTimes(cutoff).MaxTimes(numMsgs)
			}
			tcc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, b.ID())
			var wg sync.WaitGroup
			for i := 0; i < numMsgs; i++ {
				nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
				if !tc.concurrent && i < cutoff { // we know exactly which msgs will be validated
					tcc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tcc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
						Return(true, nil)
				}
				if tc.concurrent {
					wg.Add(1)
					go func(data []byte) {
						res := tcc.HandleCertifyMessage(context.TODO(), "peer", data)
						require.Equal(t, pubsub.ValidationAccept, res)
						wg.Done()
					}(encoded)
				} else {
					res := tcc.HandleCertifyMessage(context.TODO(), "peer", encoded)
					require.Equal(t, pubsub.ValidationAccept, res)
				}
			}
			wg.Wait()
			verifyCerts(t, tcc.db, b.LayerIndex, map[types.BlockID]bool{b.ID(): true, ho: false})
		})
	}
}

func Test_HandleCertifyMessage_MultipleCertificates(t *testing.T) {
	tt := []struct {
		name           string
		sameBid, valid bool
		err            error
	}{
		{
			name:    "old cert has same block ID",
			sameBid: true,
		},
		{
			name: "old cert no longer valid",
		},
		{
			name:  "old cert still valid",
			valid: true,
			err:   errMultipleCerts,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tcc := newTestCertifier(t)
			numMsgs := tcc.cfg.CommitteeSize
			cutoff := tcc.cfg.CertifyThreshold / int(defaultCnt)
			b := generateBlock(t, tcc.db)
			bid := types.RandomBlockID()
			if tc.sameBid {
				bid = b.ID()
			}
			oldCert := &types.Certificate{
				BlockID: bid,
			}
			for i := 0; i < cutoff; i++ {
				nid, msg, _ := genEncodedMsg(t, b.LayerIndex, bid)
				oldCert.Signatures = append(oldCert.Signatures, *msg)
				if !tc.sameBid {
					tcc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tcc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
						Return(tc.valid, nil)
				}
			}
			require.NoError(t, certificates.Add(tcc.db, b.LayerIndex, oldCert))

			tcc.mClk.EXPECT().GetCurrentLayer().Return(b.LayerIndex).AnyTimes()
			tcc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil).AnyTimes()
			require.NoError(t, tcc.RegisterForCert(context.TODO(), b.LayerIndex, b.ID()))
			if tc.err != nil {
				tcc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, types.EmptyBlockID)
			} else {
				tcc.mtortoise.EXPECT().OnHareOutput(b.LayerIndex, b.ID())
			}
			for i := 0; i < numMsgs; i++ {
				nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
				if i < cutoff { // we know exactly which msgs will be validated
					tcc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tcc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
						Return(true, nil)
				}
				tcc.HandleCertifyMessage(context.TODO(), "peer", encoded)
			}
			expected := map[types.BlockID]bool{}
			if tc.err == nil {
				if tc.sameBid {
					expected[b.ID()] = true
				} else {
					expected[b.ID()] = true
					expected[bid] = false
				}
			} else {
				expected[b.ID()] = true
				expected[bid] = true
			}
			verifyCerts(t, tcc.db, b.LayerIndex, expected)
		})
	}
}

func Test_HandleCertifyMessage_NotRegistered(t *testing.T) {
	tc := newTestCertifier(t)
	numMsgs := tc.cfg.CommitteeSize
	b := generateBlock(t, tc.db)
	tc.mClk.EXPECT().GetCurrentLayer().Return(b.LayerIndex).AnyTimes()
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil).AnyTimes()
	require.NoError(t, tc.RegisterForCert(context.TODO(), b.LayerIndex, types.RandomBlockID()))
	for i := 0; i < numMsgs; i++ {
		nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
		require.Equal(t, pubsub.ValidationAccept, res)
	}
	verifyCerts(t, tc.db, b.LayerIndex, nil)
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
	encoded := []byte("guaranteed corrupt")

	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationReject, res)
}

func Test_HandleCertifyMessage_LayerNotRegistered(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterForCert(context.TODO(), b.LayerIndex.Add(1), types.RandomBlockID()))
	tc.mClk.EXPECT().GetCurrentLayer().Return(b.LayerIndex).AnyTimes()
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
		Return(true, nil)
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationAccept, res)
}

func Test_HandleCertifyMessage_BlockNotRegistered(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterForCert(context.TODO(), b.LayerIndex, types.RandomBlockID()))
	tc.mClk.EXPECT().GetCurrentLayer().Return(b.LayerIndex).AnyTimes()
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mOracle.EXPECT().Validate(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
		Return(true, nil)
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationAccept, res)
}

func Test_HandleCertifyMessage_BeaconNotAvailable(t *testing.T) {
	tc := newTestCertifier(t)
	b := generateBlock(t, tc.db)
	_, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterForCert(context.TODO(), msg.LayerID, types.RandomBlockID()))
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.EmptyBeacon, errors.New("meh"))
	res := tc.HandleCertifyMessage(context.TODO(), "peer", encoded)
	require.Equal(t, pubsub.ValidationIgnore, res)
}

func Test_OldLayersPruned(t *testing.T) {
	tc := newTestCertifier(t)
	lid := types.NewLayerID(11)

	require.NoError(t, tc.RegisterForCert(context.TODO(), lid, types.RandomBlockID()))
	require.NoError(t, tc.RegisterForCert(context.TODO(), lid.Add(1), types.RandomBlockID()))
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

	extractor, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)

	tc.mOracle.EXPECT().Proof(gomock.Any(), b.LayerIndex, eligibility.CertifyRound).Return(proof, nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, tc.nodeID, proof).Return(defaultCnt, nil)
	tc.mPub.EXPECT().Publish(gomock.Any(), pubsub.BlockCertify, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var msg types.CertifyMessage
			require.NoError(t, codec.Decode(got, &msg))

			nodeId, err := extractor.ExtractNodeID(msg.Bytes(), msg.Signature)
			require.NoError(t, err)
			require.Equal(t, tc.nodeID, nodeId)
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
