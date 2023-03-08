package activation

import (
	"context"
	"testing"
	"time"

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

var id = types.NodeID{}

func getTestConfig(t *testing.T) (PostConfig, PostSetupOpts) {
	cfg := DefaultPostConfig()

	opts := DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.NumUnits = cfg.MinNumUnits
	opts.ComputeProviderID = int(initialization.CPUProviderID())

	return cfg, opts
}

func TestPostSetupManager(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

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

				if status.NumLabelsWritten < uint64(opts.NumUnits)*cfg.LabelsPerUnit {
					req.Equal(PostSetupStateInProgress, status.State)
				}
			}
		}
	})

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts))
	cancel()
	_ = eg.Wait()

	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Create data (same opts).
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Cleanup.
	req.NoError(mgr.Reset())

	// Create data (same opts, after deletion).
	req.NoError(mgr.StartSession(context.Background(), opts))
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify the initial status.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts))
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify the initial status.
	status = mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)
}

func TestPostSetupManager_GenerateProof(t *testing.T) {
	req := require.New(t)
	ch := make([]byte, 32)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(context.Background(), ch)
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Generate proof.
	p, m, err := mgr.GenerateProof(context.Background(), ch)
	req.NoError(err)

	// Verify the proof
	err = verifying.Verify(&shared.Proof{
		Nonce:   p.Nonce,
		Indices: p.Indices,
	}, &shared.ProofMetadata{
		NodeId:          id.Bytes(),
		CommitmentAtxId: goldenATXID.Bytes(),
		Challenge:       ch,
		NumUnits:        opts.NumUnits,
		BitsPerLabel:    m.BitsPerLabel,
		LabelsPerUnit:   m.LabelsPerUnit,
		K1:              m.K1,
		K2:              m.K2,
		B:               m.B,
		N:               m.N,
	})
	req.NoError(err)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(context.Background(), ch)
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_VRFNonce(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to get nonce.
	_, err = mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Get nonce.
	nonce, err := mgr.VRFNonce()
	req.NoError(err)
	req.NotZero(nonce)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to get nonce.
	_, err = mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_Stop(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify state.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Reset.
	req.NoError(mgr.Reset())

	// Verify state.
	req.Equal(PostSetupStateNotStarted, mgr.Status().State)

	// Create data again.
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)
	opts.NumUnits *= 10

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Create data.
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return mgr.StartSession(ctx, opts)
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
	req.LessOrEqual(status.NumLabelsWritten, uint64(opts.NumUnits)*cfg.LabelsPerUnit)

	// Continue to create data.
	req.NoError(mgr.StartSession(context.Background(), opts))

	// Verify status.
	status = mgr.Status()
	req.Equal(PostSetupStateComplete, status.State)
	req.Equal(uint64(opts.NumUnits)*cfg.LabelsPerUnit, status.NumLabelsWritten)
}

func TestPostSetupManager_findCommitmentAtx_UsesLatestAtx(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, _ := getTestConfig(t)

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	id := sig.NodeID()

	latestAtx := addPrevAtx(t, mgr.db, 1, sig, &id)
	atx, err := mgr.findCommitmentAtx()
	require.NoError(t, err)
	require.Equal(t, latestAtx.ID(), atx)
}

func TestPostSetupManager_findCommitmentAtx_DefaultsToGoldenAtx(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, _ := getTestConfig(t)

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	atx, err := mgr.findCommitmentAtx()
	require.NoError(t, err)
	require.Equal(t, goldenATXID, atx)
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromPostMetadata(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	id := sig.NodeID()

	// write commitment atx to metadata
	commitmentAtx := types.RandomATXID()
	initialization.SaveMetadata(opts.DataDir, &shared.PostMetadata{
		CommitmentAtxId: commitmentAtx.Bytes(),
		NodeId:          id.Bytes(),
	})

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	atxid, err := mgr.commitmentAtx(opts.DataDir)
	require.NoError(t, err)
	require.NotNil(t, atxid)
	require.Equal(t, commitmentAtx, atxid)
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromInitialAtx(t *testing.T) {
	req := require.New(t)

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	goldenATXID := types.ATXID{2, 3, 4}
	cfg, opts := getTestConfig(t)

	sig, err := signing.NewEdSigner()
	req.NoError(err)
	id := sig.NodeID()

	// add an atx by the same node
	commitmentAtx := types.RandomATXID()
	atx := types.NewActivationTx(types.NIPostChallenge{}, &id, types.Address{}, nil, 1, nil, nil)
	atx.CommitmentATX = &commitmentAtx
	vatx := addAtx(t, cdb, sig, atx)

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	atxid, err := mgr.commitmentAtx(opts.DataDir)
	require.NoError(t, err)
	require.NotNil(t, atxid)
	require.Equal(t, commitmentAtx, atxid)
	require.NotEqual(t, vatx.ID(), atx)
}
