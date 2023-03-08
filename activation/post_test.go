package activation

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestPostSetupManager(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var eg errgroup.Group
	lastStatus := &PostSetupStatus{}
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				status := mgr.Status()
				req.GreaterOrEqual(status.NumLabelsWritten, lastStatus.NumLabelsWritten)

				if status.NumLabelsWritten < uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit {
					req.Equal(PostSetupStateInProgress, status.State)
				}
			}
		}
	})

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))
	cancel()
	_ = eg.Wait()

	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Create data (same opts).
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))

	// Cleanup.
	req.NoError(mgr.Reset())

	// Create data (same opts, after deletion).
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// Verify the initial status.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Re-instantiate `PostSetupManager`.
	mgr = newTestPostManager(t)

	// Verify the initial status.
	status = mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)
}

func TestPostSetupManager_GenerateProof(t *testing.T) {
	req := require.New(t)
	ch := make([]byte, 32)

	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// Attempt to generate proof.
	_, _, err := mgr.GenerateProof(context.Background(), ch)
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))

	// Generate proof.
	p, m, err := mgr.GenerateProof(context.Background(), ch)
	req.NoError(err)

	// Verify the proof
	err = verifying.Verify(&shared.Proof{
		Nonce:   p.Nonce,
		Indices: p.Indices,
	}, &shared.ProofMetadata{
		NodeId:          mgr.id.Bytes(),
		CommitmentAtxId: mgr.goldenATXID.Bytes(),
		Challenge:       ch,
		NumUnits:        mgr.opts.NumUnits,
		BitsPerLabel:    m.BitsPerLabel,
		LabelsPerUnit:   m.LabelsPerUnit,
		K1:              m.K1,
		K2:              m.K2,
		B:               m.B,
		N:               m.N,
	})
	req.NoError(err)

	// Re-instantiate `PostSetupManager`.
	mgr = newTestPostManager(t)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(context.Background(), ch)
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_VRFNonce(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// Attempt to get nonce.
	_, err := mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))

	// Get nonce.
	nonce, err := mgr.VRFNonce()
	req.NoError(err)
	req.NotZero(nonce)

	// Re-instantiate `PostSetupManager`.
	mgr = newTestPostManager(t)

	// Attempt to get nonce.
	_, err = mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_Stop(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// Verify state.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Reset.
	req.NoError(mgr.Reset())

	// Verify state.
	req.Equal(PostSetupStateNotStarted, mgr.Status().State)

	// Create data again.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// Create data.
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return mgr.StartSession(ctx, mgr.opts)
	})

	// Wait a bit for the setup to proceed.
	time.Sleep(100 * time.Millisecond)

	// Verify the intermediate status.
	status := mgr.Status()
	req.Equal(PostSetupStateInProgress, status.State)

	// Stop initialization.
	cancel()

	req.ErrorIs(eg.Wait(), context.Canceled)

	// Verify status.
	status = mgr.Status()
	req.Equal(PostSetupStateStopped, status.State)
	req.LessOrEqual(status.NumLabelsWritten, uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit)

	// Continue to create data.
	req.NoError(mgr.StartSession(context.Background(), mgr.opts))

	// Verify status.
	status = mgr.Status()
	req.Equal(PostSetupStateComplete, status.State)
	req.Equal(uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit, status.NumLabelsWritten)
}

func TestPostSetupManager_findCommitmentAtx_UsesLatestAtx(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady)

	latestAtx := addPrevAtx(t, mgr.db, 1, mgr.signer, &mgr.id)
	atx, err := mgr.findCommitmentAtx(context.Background())
	require.NoError(t, err)
	require.Equal(t, latestAtx.ID(), atx)
}

func TestPostSetupManager_findCommitmentAtx_DefaultsToGoldenAtx(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady)

	atx, err := mgr.findCommitmentAtx(context.Background())
	require.NoError(t, err)
	require.Equal(t, mgr.goldenATXID, atx)
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromPostMetadata(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// write commitment atx to metadata
	commitmentAtx := types.RandomATXID()
	initialization.SaveMetadata(mgr.opts.DataDir, &shared.PostMetadata{
		CommitmentAtxId: commitmentAtx.Bytes(),
		NodeId:          mgr.signer.NodeID().Bytes(),
	})

	atxid, err := mgr.commitmentAtx(context.Background(), mgr.opts.DataDir)
	require.NoError(t, err)
	require.NotNil(t, atxid)
	require.Equal(t, commitmentAtx, atxid)
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromInitialAtx(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.msync.EXPECT().RegisterForATXSynced().DoAndReturn(atxReady).AnyTimes()

	// add an atx by the same node
	commitmentAtx := types.RandomATXID()
	id := mgr.signer.NodeID()
	atx := types.NewActivationTx(types.NIPostChallenge{}, &id, types.Address{}, nil, 1, nil, nil)
	atx.CommitmentATX = &commitmentAtx
	addAtx(t, mgr.cdb, mgr.signer, atx)

	atxid, err := mgr.commitmentAtx(context.Background(), mgr.opts.DataDir)
	require.NoError(t, err)
	require.Equal(t, commitmentAtx, atxid)
}

type testPostManager struct {
	*PostSetupManager

	opts PostSetupOpts

	signer *signing.EdSigner
	cdb    *datastore.CachedDB

	// Mocks.
	msync *Mocksyncer
}

func newTestPostManager(tb testing.TB) *testPostManager {
	tb.Helper()

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()

	cfg := DefaultPostConfig()

	opts := DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.NumUnits = cfg.MaxNumUnits
	opts.ComputeProviderID = int(initialization.CPUProviderID())

	goldenATXID := types.ATXID{2, 3, 4}

	msync := NewMocksyncer(gomock.NewController(tb))
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(tb), cdb, msync, goldenATXID)
	require.NoError(tb, err)

	return &testPostManager{
		PostSetupManager: mgr,
		opts:             opts,
		signer:           sig,
		cdb:              cdb,
		msync:            msync,
	}
}

func atxReady() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
