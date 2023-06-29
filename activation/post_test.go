package activation

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/spacemeshos/post/config"
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
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))
	cancel()
	_ = eg.Wait()

	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Create data (same opts).
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))

	// Cleanup.
	req.NoError(mgr.Reset())

	// Create data (same opts, after deletion).
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

// Checks that PrepareInitializer returns an error when invalid opts are given.
// It's not exhaustive since this validation occurs in the post repo codebase
// and should be fully tested there but we check a few cases to be sure that
// PrepareInitializer will return errors when the opts don't validate.
func TestPostSetupManager_PrepareInitializer(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// check no error with good options.
	req.NoError(mgr.PrepareInitializer(ctx, mgr.opts))

	defaultConfig := config.DefaultConfig()

	// Check that invalid options return errors
	opts := mgr.opts
	opts.ComputeBatchSize = 3
	req.Error(mgr.PrepareInitializer(ctx, opts))

	opts = mgr.opts
	opts.NumUnits = defaultConfig.MaxNumUnits + 1
	req.Error(mgr.PrepareInitializer(ctx, opts))

	opts = mgr.opts
	opts.NumUnits = defaultConfig.MinNumUnits - 1
	req.Error(mgr.PrepareInitializer(ctx, opts))

	opts = mgr.opts
	opts.Scrypt.N = 0
	req.Error(opts.Scrypt.Validate())
	req.Error(mgr.PrepareInitializer(ctx, opts))
}

func TestPostSetupManager_PrepareInitializer_BestProvider(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	mgr.opts.ProviderID = config.BestProviderID
	req.NoError(mgr.PrepareInitializer(ctx, mgr.opts))
}

// Checks that the sequence of calls for initialization (first
// PrepareInitializer and then StartSession) is enforced.
func TestPostSetupManager_InitializationCallSequence(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// Should fail since we have not prepared.
	req.Error(mgr.StartSession(ctx))

	req.NoError(mgr.PrepareInitializer(ctx, mgr.opts))

	// Should fail since we need to call StartSession after PrepareInitializer.
	req.Error(mgr.PrepareInitializer(ctx, mgr.opts))

	req.NoError(mgr.StartSession(ctx))

	// Should fail since it is required to call PrepareInitializer before each
	// call to StartSession.
	req.Error(mgr.StartSession(ctx))
}

func TestPostSetupManager_StateError(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.opts.NumUnits = 0
	req.Error(mgr.PrepareInitializer(context.Background(), mgr.opts))
	// Verify Status returns StateError
	req.Equal(PostSetupStateError, mgr.Status().State)
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)

	// Verify the initial status.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))
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

	// Attempt to generate proof.
	_, _, err := mgr.GenerateProof(context.Background(), ch)
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))

	// Generate proof.
	p, m, err := mgr.GenerateProof(context.Background(), ch)
	req.NoError(err)

	// Verify the proof
	verifier, err := verifying.NewProofVerifier()
	req.NoError(err)
	defer verifier.Close()
	err = verifier.Verify(&shared.Proof{
		Nonce:   p.Nonce,
		Indices: p.Indices,
		Pow:     p.Pow,
	}, &shared.ProofMetadata{
		NodeId:          mgr.id.Bytes(),
		CommitmentAtxId: mgr.goldenATXID.Bytes(),
		Challenge:       ch,
		NumUnits:        mgr.opts.NumUnits,
		LabelsPerUnit:   m.LabelsPerUnit,
	},
		config.DefaultConfig(),
		logtest.New(t).WithName("verifying").Zap(),
		verifying.WithLabelScryptParams(mgr.opts.Scrypt),
	)
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

	// Attempt to get nonce.
	_, err := mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)

	// Create data.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))

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

	// Verify state.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Reset.
	req.NoError(mgr.Reset())

	// Verify state.
	req.Equal(PostSetupStateNotStarted, mgr.Status().State)

	// Create data again.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	req := require.New(t)

	mgr := newTestPostManager(t)
	mgr.opts.MaxFileSize = 4096
	mgr.opts.NumUnits = mgr.cfg.MaxNumUnits

	// Create data.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return mgr.StartSession(ctx)
	})

	// Verify the intermediate status.
	req.Eventually(func() bool {
		return mgr.Status().State == PostSetupStateInProgress
	}, 5*time.Second, 10*time.Millisecond)

	// Stop initialization.
	cancel()

	req.ErrorIs(eg.Wait(), context.Canceled)

	// Verify status.
	status := mgr.Status()
	req.Equal(PostSetupStateStopped, status.State)
	req.LessOrEqual(status.NumLabelsWritten, uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit)

	// Continue to create data.
	req.NoError(mgr.PrepareInitializer(context.Background(), mgr.opts))
	req.NoError(mgr.StartSession(context.Background()))

	// Verify status.
	status = mgr.Status()
	req.Equal(PostSetupStateComplete, status.State)
	req.Equal(uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit, status.NumLabelsWritten)
}

func TestPostSetupManager_findCommitmentAtx_UsesLatestAtx(t *testing.T) {
	mgr := newTestPostManager(t)

	latestAtx := addPrevAtx(t, mgr.db, 1, mgr.signer)
	atx, err := mgr.findCommitmentAtx(context.Background())
	require.NoError(t, err)
	require.Equal(t, latestAtx.ID(), atx)
}

func TestPostSetupManager_findCommitmentAtx_DefaultsToGoldenAtx(t *testing.T) {
	mgr := newTestPostManager(t)

	atx, err := mgr.findCommitmentAtx(context.Background())
	require.NoError(t, err)
	require.Equal(t, mgr.goldenATXID, atx)
}

func TestPostSetupManager_Providers_includesCPU(t *testing.T) {
	mgr := newTestPostManager(t)

	providers, err := mgr.Providers()
	require.NoError(t, err)

	for _, p := range providers {
		if p.ID == initialization.CPUProviderID() {
			return
		}
	}
	require.Fail(t, "no CPU provider found")
}

func TestPostSetupManager_BestProvider(t *testing.T) {
	mgr := newTestPostManager(t)

	providers, err := mgr.BestProvider()
	require.NoError(t, err)
	require.NotNil(t, providers)
}

func TestPostSetupManager_Benchmark(t *testing.T) {
	mgr := newTestPostManager(t)

	providers, err := mgr.Providers()
	require.NoError(t, err)

	for _, p := range providers {
		score, err := mgr.Benchmark(p)
		require.NoError(t, err)
		require.NotZero(t, score)
	}
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromPostMetadata(t *testing.T) {
	mgr := newTestPostManager(t)

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

	// add an atx by the same node
	commitmentAtx := types.RandomATXID()
	atx := types.NewActivationTx(types.NIPostChallenge{}, types.Address{}, nil, 1, nil)
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
}

func newTestPostManager(tb testing.TB) *testPostManager {
	tb.Helper()

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()

	cfg := DefaultPostConfig()

	opts := DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.ProviderID = int(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	goldenATXID := types.ATXID{2, 3, 4}

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	provingOpts := DefaultPostProvingOpts()
	provingOpts.Flags = config.RecommendedPowFlags()
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(tb), cdb, goldenATXID, provingOpts)
	require.NoError(tb, err)

	return &testPostManager{
		PostSetupManager: mgr,
		opts:             opts,
		signer:           sig,
		cdb:              cdb,
	}
}

func TestSettingPowDifficulty(t *testing.T) {
	t.Parallel()
	expected := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 8)
	encoded := hex.EncodeToString(expected)
	t.Run("parse 32B hex", func(t *testing.T) {
		t.Parallel()
		d := PowDifficulty{}
		err := d.Set(encoded)
		require.NoError(t, err)
		require.Equal(t, expected, d[:])
	})
	t.Run("input too short", func(t *testing.T) {
		t.Parallel()
		d := PowDifficulty{}
		require.Error(t, d.Set("123"))
		require.Equal(t, PowDifficulty{}, d)
	})
	t.Run("input too long", func(t *testing.T) {
		t.Parallel()
		d := PowDifficulty{}
		require.Error(t, d.Set(hex.EncodeToString(bytes.Repeat([]byte{0x01}, 33))))
		require.Equal(t, PowDifficulty{}, d)
	})
	t.Run("not a hex string", func(t *testing.T) {
		t.Parallel()
		encoded := encoded[:len(encoded)-1] + "G"
		d := PowDifficulty{}
		require.Error(t, d.Set(encoded))
		require.Equal(t, PowDifficulty{}, d)
	})
}
