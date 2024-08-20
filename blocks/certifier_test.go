package blocks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmock "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const defaultCnt = uint16(2)

type testCertifier struct {
	*Certifier
	db        sql.StateDatabase
	mOracle   *eligibility.MockRolacle
	mPub      *pubsubmock.MockPublisher
	mClk      *mocks.MocklayerClock
	mb        *smocks.MockBeaconGetter
	mTortoise *smocks.MockTortoise
}

func newTestCertifier(t *testing.T, signers int) *testCertifier {
	t.Helper()
	types.SetLayersPerEpoch(3)
	db := statesql.InMemory()
	ctrl := gomock.NewController(t)
	mo := eligibility.NewMockRolacle(ctrl)
	mp := pubsubmock.NewMockPublisher(ctrl)
	mc := mocks.NewMocklayerClock(ctrl)
	mb := smocks.NewMockBeaconGetter(ctrl)
	mtortoise := smocks.NewMockTortoise(ctrl)
	c := NewCertifier(db, mo, signing.NewEdVerifier(), mp, mc, mb, mtortoise,
		WithCertifierLogger(zaptest.NewLogger(t)),
	)
	for i := 0; i < signers; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		c.Register(signer)
	}

	return &testCertifier{
		Certifier: c,
		db:        db,
		mOracle:   mo,
		mPub:      mp,
		mClk:      mc,
		mb:        mb,
		mTortoise: mtortoise,
	}
}

func generateBlock(t *testing.T, db sql.Executor) *types.Block {
	t.Helper()
	block := types.NewExistingBlock(
		types.RandomBlockID(),
		types.InnerBlock{LayerIndex: types.LayerID(11)},
	)
	require.NoError(t, blocks.Add(db, block))
	return block
}

func genCertifyMsg(
	tb testing.TB,
	lid types.LayerID,
	bid types.BlockID,
	cnt uint16,
) (types.NodeID, *types.CertifyMessage) {
	tb.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	msg := &types.CertifyMessage{
		CertifyContent: types.CertifyContent{
			LayerID:        lid,
			BlockID:        bid,
			EligibilityCnt: cnt,
			Proof:          types.RandomVrfSignature(),
		},
		SmesherID: signer.NodeID(),
	}
	msg.Signature = signer.Sign(signing.HARE, msg.Bytes())
	return signer.NodeID(), msg
}

func genEncodedMsg(
	t *testing.T,
	lid types.LayerID,
	bid types.BlockID,
) (types.NodeID, *types.CertifyMessage, []byte) {
	t.Helper()
	nid, msg := genCertifyMsg(t, lid, bid, defaultCnt)
	data, err := codec.Encode(msg)
	require.NoError(t, err)
	return nid, msg, data
}

func verifyCerts(
	t *testing.T,
	db sql.Executor,
	lid types.LayerID,
	expected map[types.BlockID]bool,
) {
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
	tc := newTestCertifier(t, 1)
	lid := types.LayerID(11)
	ch := make(chan struct{}, 1)
	tc.mClk.EXPECT().CurrentLayer().Return(lid).AnyTimes()
	tc.mClk.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(
		func(_ types.LayerID) <-chan struct{} {
			return ch
		}).AnyTimes()
	tc.Start(context.Background())
	ch <- struct{}{}
	tc.Start(context.Background()) // calling Start() for the second time have no effect
	tc.Stop()
}

func Test_HandleSyncedCertificate(t *testing.T) {
	tc := newTestCertifier(t, 1)
	numMsgs := tc.cfg.CertifyThreshold / int(defaultCnt)
	b := generateBlock(t, tc.db)
	sigs := make([]types.CertifyMessage, numMsgs)
	for i := 0; i < numMsgs; i++ {
		nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(
			gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		sigs[i] = *msg
	}
	cert := &types.Certificate{
		BlockID:    b.ID(),
		Signatures: sigs,
	}
	require.NoError(t, tc.HandleSyncedCertificate(context.Background(), b.LayerIndex, cert))
	verifyCerts(t, tc.db, b.LayerIndex, map[types.BlockID]bool{b.ID(): true})
	require.Equal(t, map[types.EpochID]int{b.LayerIndex.GetEpoch(): 1}, tc.CertCount())
}

func Test_HandleSyncedCertificate_HareOutputTrumped(t *testing.T) {
	tc := newTestCertifier(t, 1)
	numMsgs := tc.cfg.CertifyThreshold / int(defaultCnt)
	b := generateBlock(t, tc.db)
	sigs := make([]types.CertifyMessage, numMsgs)
	for i := 0; i < numMsgs; i++ {
		nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(
			gomock.Any(),
			b.LayerIndex,
			eligibility.CertifyRound,
			tc.cfg.CommitteeSize,
			nid,
			msg.Proof,
			defaultCnt,
		).
			Return(true, nil)
		sigs[i] = *msg
	}
	cert := &types.Certificate{
		BlockID:    b.ID(),
		Signatures: sigs,
	}
	ho := types.RandomBlockID()
	require.NoError(t, certificates.SetHareOutput(tc.db, b.LayerIndex, ho))
	require.NoError(t, tc.HandleSyncedCertificate(context.Background(), b.LayerIndex, cert))
	verifyCerts(t, tc.db, b.LayerIndex, map[types.BlockID]bool{b.ID(): true, ho: false})
	require.Equal(t, map[types.EpochID]int{b.LayerIndex.GetEpoch(): 1}, tc.CertCount())
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
		t.Run(tc.name, func(t *testing.T) {
			tcc := newTestCertifier(t, 1)
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
					tcc.mOracle.EXPECT().Validate(gomock.Any(),
						b.LayerIndex,
						eligibility.CertifyRound,
						tcc.cfg.CommitteeSize,
						nid,
						msg.Proof,
						defaultCnt,
					).Return(tc.valid, nil)
				}
			}
			require.NoError(t, certificates.Add(tcc.db, b.LayerIndex, oldCert))

			sigs := make([]types.CertifyMessage, numMsgs)
			for i := 0; i < numMsgs; i++ {
				nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
				tcc.mOracle.EXPECT().Validate(
					gomock.Any(),
					b.LayerIndex,
					eligibility.CertifyRound,
					tcc.cfg.CommitteeSize,
					nid,
					msg.Proof,
					defaultCnt,
				).Return(true, nil)
				sigs[i] = *msg
			}
			cert := &types.Certificate{
				BlockID:    b.ID(),
				Signatures: sigs,
			}
			if tc.err != nil {
				tcc.mTortoise.EXPECT().OnHareOutput(b.LayerIndex, types.EmptyBlockID)
			}
			require.ErrorIs(
				t,
				tcc.HandleSyncedCertificate(context.Background(), b.LayerIndex, cert),
				tc.err,
			)
			expected := map[types.BlockID]bool{}
			if tc.err == nil {
				if tc.sameBid {
					expected[b.ID()] = true
				} else {
					expected[b.ID()] = true
					expected[bid] = false
				}
				require.Equal(t, map[types.EpochID]int{b.LayerIndex.GetEpoch(): 1}, tcc.CertCount())
			} else {
				expected[b.ID()] = true
				expected[bid] = true
				require.Empty(t, tcc.CertCount())
			}
			verifyCerts(t, tcc.db, b.LayerIndex, expected)
		})
	}
}

func Test_HandleSyncedCertificate_NotEnoughEligibility(t *testing.T) {
	tc := newTestCertifier(t, 1)
	numMsgs := tc.cfg.CertifyThreshold/int(defaultCnt) - 1
	b := generateBlock(t, tc.db)
	require.NoError(t, tc.RegisterForCert(context.Background(), b.LayerIndex, b.ID()))
	sigs := make([]types.CertifyMessage, numMsgs)
	for i := 0; i < numMsgs; i++ {
		nid, msg, _ := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(
			gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		sigs[i] = *msg
	}
	cert := &types.Certificate{
		BlockID:    b.ID(),
		Signatures: sigs,
	}
	require.ErrorIs(
		t,
		tc.HandleSyncedCertificate(context.Background(), b.LayerIndex, cert),
		errInvalidCert,
	)
	require.Empty(t, tc.CertCount())
}

func nilErr(err error) bool {
	return err == nil
}

func isErr(err error) bool {
	return err != nil
}

func Test_HandleCertifyMessage(t *testing.T) {
	cfg := defaultCertConfig()
	lid := types.LayerID(10)
	tt := []struct {
		name     string
		diff     int
		expected func(error) bool
	}{
		{
			name:     "on time",
			expected: nilErr,
		},
		{
			name:     "genesis",
			expected: isErr,
			diff:     -10,
		},
		{
			name:     "boundary - early",
			expected: nilErr,
			diff:     -1 * int(cfg.LayerBuffer),
		},
		{
			name:     "boundary - late",
			expected: nilErr,
			diff:     int(cfg.LayerBuffer + 1),
		},
		{
			name:     "too early",
			expected: isErr,
			diff:     -1 * int(cfg.LayerBuffer+1),
		},
		{
			name:     "too late",
			expected: isErr,
			diff:     int(cfg.LayerBuffer + 2),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			testCert := newTestCertifier(t, 1)
			b := generateBlock(t, testCert.db)
			b.LayerIndex = lid
			nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

			current := lid
			if tc.diff > 0 {
				current = current.Add(uint32(tc.diff))
			} else {
				current = current.Sub(uint32(-1 * tc.diff))
			}
			testCert.mClk.EXPECT().CurrentLayer().Return(current).AnyTimes()
			testCert.mb.EXPECT().
				GetBeacon(b.LayerIndex.GetEpoch()).
				Return(types.RandomBeacon(), nil)
			require.NoError(t, testCert.RegisterForCert(context.Background(), b.LayerIndex, b.ID()))
			testCert.mOracle.EXPECT().Validate(
				gomock.Any(),
				b.LayerIndex,
				eligibility.CertifyRound,
				testCert.cfg.CommitteeSize,
				nid,
				msg.Proof,
				defaultCnt,
			).Return(true, nil).AnyTimes()
			res := testCert.HandleCertifyMessage(context.Background(), "peer", encoded)
			require.True(t, tc.expected(res))
			require.Empty(t, testCert.CertCount())
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
		t.Run(tc.name, func(t *testing.T) {
			tcc := newTestCertifier(t, 1)
			numMsgs := tcc.cfg.CommitteeSize
			cutoff := tcc.cfg.CertifyThreshold / int(defaultCnt)
			b := generateBlock(t, tcc.db)
			ho := types.RandomBlockID()
			require.NoError(t, certificates.SetHareOutput(tcc.db, b.LayerIndex, ho))
			tcc.mClk.EXPECT().CurrentLayer().Return(b.LayerIndex).AnyTimes()
			tcc.mb.EXPECT().
				GetBeacon(b.LayerIndex.GetEpoch()).
				Return(types.RandomBeacon(), nil).
				AnyTimes()
			require.NoError(t, tcc.RegisterForCert(context.Background(), b.LayerIndex, b.ID()))
			if tc.concurrent {
				tcc.mOracle.EXPECT().Validate(gomock.Any(),
					b.LayerIndex,
					eligibility.CertifyRound,
					tcc.cfg.CommitteeSize,
					gomock.Any(),
					gomock.Any(),
					defaultCnt,
				).Return(true, nil).MinTimes(cutoff).MaxTimes(numMsgs)
			}
			var eg errgroup.Group
			for i := 0; i < numMsgs; i++ {
				nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
				if !tc.concurrent && i < cutoff { // we know exactly which msgs will be validated
					tcc.mOracle.EXPECT().Validate(
						gomock.Any(),
						b.LayerIndex,
						eligibility.CertifyRound,
						tcc.cfg.CommitteeSize,
						nid,
						msg.Proof,
						defaultCnt,
					).Return(true, nil)
				}
				if tc.concurrent {
					eg.Go(func() error {
						return tcc.HandleCertifyMessage(context.Background(), "peer", encoded)
					})
				} else {
					res := tcc.HandleCertifyMessage(context.Background(), "peer", encoded)
					require.NoError(t, res)
				}
			}
			require.NoError(t, eg.Wait())
			verifyCerts(t, tcc.db, b.LayerIndex, map[types.BlockID]bool{b.ID(): true, ho: false})
			require.Equal(t, map[types.EpochID]int{b.LayerIndex.GetEpoch(): 1}, tcc.CertCount())
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
		t.Run(tc.name, func(t *testing.T) {
			tcc := newTestCertifier(t, 1)
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
					tcc.mOracle.EXPECT().Validate(
						gomock.Any(),
						b.LayerIndex,
						eligibility.CertifyRound,
						tcc.cfg.CommitteeSize,
						nid,
						msg.Proof,
						defaultCnt,
					).Return(tc.valid, nil)
				}
			}
			require.NoError(t, certificates.Add(tcc.db, b.LayerIndex, oldCert))

			tcc.mClk.EXPECT().CurrentLayer().Return(b.LayerIndex).AnyTimes()
			tcc.mb.EXPECT().
				GetBeacon(b.LayerIndex.GetEpoch()).
				Return(types.RandomBeacon(), nil).
				AnyTimes()
			require.NoError(t, tcc.RegisterForCert(context.Background(), b.LayerIndex, b.ID()))
			if tc.err != nil {
				tcc.mTortoise.EXPECT().OnHareOutput(b.LayerIndex, types.EmptyBlockID)
			}
			for i := 0; i < numMsgs; i++ {
				nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
				if i < cutoff { // we know exactly which msgs will be validated
					tcc.mOracle.EXPECT().
						Validate(
							gomock.Any(),
							b.LayerIndex,
							eligibility.CertifyRound,
							tcc.cfg.CommitteeSize,
							nid,
							msg.Proof,
							defaultCnt,
						).
						Return(true, nil)
				}
				tcc.HandleCertifyMessage(context.Background(), "peer", encoded)
			}
			expected := map[types.BlockID]bool{}
			if tc.err == nil {
				if tc.sameBid {
					expected[b.ID()] = true
				} else {
					expected[b.ID()] = true
					expected[bid] = false
				}
				require.Equal(t, map[types.EpochID]int{b.LayerIndex.GetEpoch(): 1}, tcc.CertCount())
			} else {
				expected[b.ID()] = true
				expected[bid] = true
				require.Empty(t, tcc.CertCount())
			}
			verifyCerts(t, tcc.db, b.LayerIndex, expected)
		})
	}
}

func Test_HandleCertifyMessage_NotRegistered(t *testing.T) {
	tc := newTestCertifier(t, 1)
	numMsgs := tc.cfg.CommitteeSize
	b := generateBlock(t, tc.db)
	tc.mClk.EXPECT().CurrentLayer().Return(b.LayerIndex).AnyTimes()
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil).AnyTimes()
	require.NoError(
		t,
		tc.RegisterForCert(context.Background(), b.LayerIndex, types.RandomBlockID()),
	)
	for i := 0; i < numMsgs; i++ {
		nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())
		tc.mOracle.EXPECT().Validate(
			gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
			Return(true, nil)
		res := tc.HandleCertifyMessage(context.Background(), "peer", encoded)
		require.NoError(t, res)
	}
	verifyCerts(t, tc.db, b.LayerIndex, nil)
	require.Empty(t, tc.CertCount())
}

func Test_HandleCertifyMessage_Stopped(t *testing.T) {
	tc := newTestCertifier(t, 1)
	tc.Stop()
	_, _, encoded := genEncodedMsg(t, types.LayerID(11), types.RandomBlockID())

	res := tc.HandleCertifyMessage(context.Background(), "peer", encoded)
	require.Error(t, res)
}

func Test_HandleCertifyMessage_CorruptedMsg(t *testing.T) {
	tc := newTestCertifier(t, 1)
	encoded := []byte("guaranteed corrupt")

	res := tc.HandleCertifyMessage(context.Background(), "peer", encoded)
	require.ErrorIs(t, res, pubsub.ErrValidationReject)
}

func Test_HandleCertifyMessage_LayerNotRegistered(t *testing.T) {
	tc := newTestCertifier(t, 1)
	b := generateBlock(t, tc.db)
	nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(
		t,
		tc.RegisterForCert(context.Background(), b.LayerIndex.Add(1), types.RandomBlockID()),
	)
	tc.mClk.EXPECT().CurrentLayer().Return(b.LayerIndex).AnyTimes()
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mOracle.EXPECT().Validate(
		gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
		Return(true, nil)
	res := tc.HandleCertifyMessage(context.Background(), "peer", encoded)
	require.NoError(t, res)
}

func Test_HandleCertifyMessage_BlockNotRegistered(t *testing.T) {
	tc := newTestCertifier(t, 1)
	b := generateBlock(t, tc.db)
	nid, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(
		t,
		tc.RegisterForCert(context.Background(), b.LayerIndex, types.RandomBlockID()),
	)
	tc.mClk.EXPECT().CurrentLayer().Return(b.LayerIndex).AnyTimes()
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mOracle.EXPECT().Validate(
		gomock.Any(), b.LayerIndex, eligibility.CertifyRound, tc.cfg.CommitteeSize, nid, msg.Proof, defaultCnt).
		Return(true, nil)
	res := tc.HandleCertifyMessage(context.Background(), "peer", encoded)
	require.NoError(t, res)
}

func Test_HandleCertifyMessage_BeaconNotAvailable(t *testing.T) {
	tc := newTestCertifier(t, 1)
	b := generateBlock(t, tc.db)
	_, msg, encoded := genEncodedMsg(t, b.LayerIndex, b.ID())

	require.NoError(t, tc.RegisterForCert(context.Background(), msg.LayerID, types.RandomBlockID()))
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.EmptyBeacon, errors.New("meh"))
	res := tc.HandleCertifyMessage(context.Background(), "peer", encoded)
	require.Error(t, res)
}

func Test_OldLayersPruned(t *testing.T) {
	tc := newTestCertifier(t, 1)
	lid := types.LayerID(11)

	require.NoError(t, tc.RegisterForCert(context.Background(), lid, types.RandomBlockID()))
	require.NoError(t, tc.RegisterForCert(context.Background(), lid.Add(1), types.RandomBlockID()))
	require.Equal(t, 2, tc.NumCached())

	current := lid.Add(tc.cfg.NumLayersToKeep + 1)
	ch := make(chan struct{}, 1)
	pruned := make(chan struct{}, 1)
	tc.mClk.EXPECT().CurrentLayer().Return(current).AnyTimes()
	tc.mClk.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(
		func(got types.LayerID) <-chan struct{} {
			if got == current.Add(1) {
				close(pruned)
			}
			return ch
		}).AnyTimes()
	tc.Start(context.Background())
	ch <- struct{}{} // for current
	ch <- struct{}{} // for current+1
	<-pruned
	require.Equal(t, 1, tc.NumCached())
	tc.Stop()
}

func Test_CertifyIfEligible(t *testing.T) {
	numSigners := 3
	tc := newTestCertifier(t, numSigners)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)

	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(),
		b.LayerIndex,
		eligibility.CertifyRound,
		tc.cfg.CommitteeSize,
		gomock.Any(),
		gomock.Any(),
	).Times(numSigners).Return(defaultCnt, nil)
	tc.mPub.EXPECT().
		Publish(gomock.Any(), pubsub.BlockCertify, gomock.Any()).
		Times(numSigners).
		DoAndReturn(
			func(_ context.Context, _ string, got []byte) error {
				var msg types.CertifyMessage
				require.NoError(t, codec.Decode(got, &msg))

				ok := signing.NewEdVerifier().
					Verify(signing.HARE, msg.SmesherID, msg.Bytes(), msg.Signature)
				require.True(t, ok)
				require.Equal(t, b.LayerIndex, msg.LayerID)
				require.Equal(t, b.ID(), msg.BlockID)
				require.Equal(t, defaultCnt, msg.EligibilityCnt)
				return nil
			})
	require.NoError(t, tc.CertifyIfEligible(context.Background(), b.LayerIndex, b.ID()))
}

func Test_CertifyIfEligible_NotEligible(t *testing.T) {
	tc := newTestCertifier(t, 1)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(),
		b.LayerIndex,
		eligibility.CertifyRound,
		tc.cfg.CommitteeSize,
		gomock.Any(),
		gomock.Any(),
	).Return(0, nil)
	require.NoError(t, tc.CertifyIfEligible(context.Background(), b.LayerIndex, b.ID()))
}

func Test_CertifyIfEligible_EligibilityErr(t *testing.T) {
	tc := newTestCertifier(t, 1)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.RandomBeacon(), nil)
	errUnknown := errors.New("unknown")
	tc.mOracle.EXPECT().CalcEligibility(gomock.Any(),
		b.LayerIndex,
		eligibility.CertifyRound,
		tc.cfg.CommitteeSize,
		gomock.Any(),
		gomock.Any(),
	).Return(0, errUnknown)
	require.ErrorIs(t, tc.CertifyIfEligible(context.Background(), b.LayerIndex, b.ID()), errUnknown)
}

func Test_CertifyIfEligible_BeaconNotAvailable(t *testing.T) {
	tc := newTestCertifier(t, 1)
	b := generateBlock(t, tc.db)
	tc.mb.EXPECT().GetBeacon(b.LayerIndex.GetEpoch()).Return(types.EmptyBeacon, errors.New("meh"))
	require.ErrorIs(
		t,
		tc.CertifyIfEligible(context.Background(), b.LayerIndex, b.ID()),
		errBeaconNotAvailable,
	)
}

func TestNewCertifyMsg(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	proof := types.RandomVrfSignature()
	blockID := types.RandomBlockID()

	msg := newCertifyMsg(signer, types.LayerID(1), blockID, proof, 77)

	require.Equal(t, types.LayerID(1), msg.LayerID)
	require.Equal(t, blockID, msg.BlockID)
	require.Equal(t, proof, msg.Proof)
	require.Equal(t, uint16(77), msg.EligibilityCnt)
	require.Equal(t, signer.NodeID(), msg.SmesherID)

	require.True(t, signing.NewEdVerifier().Verify(signing.HARE, msg.SmesherID, msg.Bytes(), msg.Signature))
}
