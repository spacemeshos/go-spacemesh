package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestPostSetupManager(t *testing.T) {
	mgr := newTestPostManager(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var eg errgroup.Group
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		lastStatus := &PostSetupStatus{}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				status := mgr.Status()
				require.GreaterOrEqual(t, status.NumLabelsWritten, lastStatus.NumLabelsWritten)

				if status.NumLabelsWritten == uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit {
					return nil
				}
				require.Contains(t, []PostSetupState{PostSetupStatePrepared, PostSetupStateInProgress}, status.State)
				lastStatus = status
			}
		}
	})

	// Create data.
	nodeID := types.RandomNodeID()
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))
	require.NoError(t, eg.Wait())
	require.Equal(t, PostSetupStateComplete, mgr.Status().State)

	// Create data (same opts).
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))

	// Cleanup.
	require.NoError(t, mgr.Reset())

	// Create data (same opts, after deletion).
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))
	require.Equal(t, PostSetupStateComplete, mgr.Status().State)
}

// Checks that PrepareInitializer returns an error when invalid opts are given.
// It's not exhaustive since this validation occurs in the post repo codebase
// and should be fully tested there but we check a few cases to be sure that
// PrepareInitializer will return errors when the opts don't validate.
func TestPostSetupManager_PrepareInitializer(t *testing.T) {
	mgr := newTestPostManager(t)
	nodeID := types.RandomNodeID()

	// check no error with good options.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))

	defaultConfig := config.DefaultConfig()

	// Check that invalid options return errors
	opts := mgr.opts
	opts.ComputeBatchSize = 3
	require.Error(t, mgr.PrepareInitializer(context.Background(), opts, nodeID))

	opts = mgr.opts
	opts.NumUnits = defaultConfig.MaxNumUnits + 1
	require.Error(t, mgr.PrepareInitializer(context.Background(), opts, nodeID))

	opts = mgr.opts
	opts.NumUnits = defaultConfig.MinNumUnits - 1
	require.Error(t, mgr.PrepareInitializer(context.Background(), opts, nodeID))

	opts = mgr.opts
	opts.Scrypt.N = 0
	require.Error(t, opts.Scrypt.Validate())
	require.Error(t, mgr.PrepareInitializer(context.Background(), opts, nodeID))
}

func TestPostSetupManager_StartSession_WithoutProvider_Error(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.opts.ProviderID.value = nil

	nodeID := types.RandomNodeID()

	// Create data.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID)) // prepare works without provider
	require.ErrorContains(t, mgr.StartSession(context.Background(), nodeID), "no provider specified")

	require.Equal(t, PostSetupStateError, mgr.Status().State)
}

func TestPostSetupManager_StartSession_WithoutProviderAfterInit_OK(t *testing.T) {
	mgr := newTestPostManager(t)
	nodeID := types.RandomNodeID()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Create data.
	require.NoError(t, mgr.PrepareInitializer(ctx, mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(ctx, nodeID))

	require.Equal(t, PostSetupStateComplete, mgr.Status().State)
	cancel()

	// start Initializer again, but with no provider set
	mgr.opts.ProviderID.value = nil

	ctx, cancel = context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	require.NoError(t, mgr.PrepareInitializer(ctx, mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(ctx, nodeID))

	require.Equal(t, PostSetupStateComplete, mgr.Status().State)
}

// Checks that the sequence of calls for initialization (first
// PrepareInitializer and then StartSession) is enforced.
func TestPostSetupManager_InitializationCallSequence(t *testing.T) {
	mgr := newTestPostManager(t)
	nodeID := types.RandomNodeID()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// Should fail since we have not prepared.
	require.Error(t, mgr.StartSession(ctx, nodeID))

	require.NoError(t, mgr.PrepareInitializer(ctx, mgr.opts, nodeID))

	// Should fail since we need to call StartSession after PrepareInitializer.
	require.Error(t, mgr.PrepareInitializer(ctx, mgr.opts, nodeID))

	require.NoError(t, mgr.StartSession(ctx, nodeID))

	// Should fail since it is required to call PrepareInitializer before each
	// call to StartSession.
	require.Error(t, mgr.StartSession(ctx, nodeID))
}

func TestPostSetupManager_StateError(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.opts.NumUnits = 0
	nodeID := types.RandomNodeID()

	require.Error(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	// Verify Status returns StateError
	require.Equal(t, PostSetupStateError, mgr.Status().State)
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	mgr := newTestPostManager(t)
	nodeID := types.RandomNodeID()

	// Verify the initial status.
	status := mgr.Status()
	require.Equal(t, PostSetupStateNotStarted, status.State)
	require.Zero(t, status.NumLabelsWritten)

	// Create data.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))
	require.Equal(t, PostSetupStateComplete, mgr.Status().State)

	// Re-instantiate `PostSetupManager`.
	mgr = newTestPostManager(t)

	// Verify the initial status.
	status = mgr.Status()
	require.Equal(t, PostSetupStateNotStarted, status.State)
	require.Zero(t, status.NumLabelsWritten)
}

func TestPostSetupManager_Stop(t *testing.T) {
	mgr := newTestPostManager(t)
	nodeID := types.RandomNodeID()

	// Verify state.
	status := mgr.Status()
	require.Equal(t, PostSetupStateNotStarted, status.State)
	require.Zero(t, status.NumLabelsWritten)

	// Create data.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))

	// Verify state.
	require.Equal(t, PostSetupStateComplete, mgr.Status().State)

	// Reset.
	require.NoError(t, mgr.Reset())

	// Verify state.
	require.Equal(t, PostSetupStateNotStarted, mgr.Status().State)

	// Create data again.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))

	// Verify state.
	require.Equal(t, PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	mgr := newTestPostManager(t)
	mgr.opts.MaxFileSize = 4096
	mgr.opts.NumUnits = mgr.cfg.MaxNumUnits

	nodeID := types.RandomNodeID()

	// Create data.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return mgr.StartSession(ctx, nodeID)
	})

	// Verify the intermediate status.
	require.Eventually(t, func() bool {
		return mgr.Status().State == PostSetupStateInProgress
	}, 5*time.Second, 10*time.Millisecond)

	// Stop initialization.
	cancel()

	require.ErrorIs(t, eg.Wait(), context.Canceled)

	// Verify status.
	status := mgr.Status()
	require.Equal(t, PostSetupStateStopped, status.State)
	require.LessOrEqual(t, status.NumLabelsWritten, uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit)

	// Continue to create data.
	require.NoError(t, mgr.PrepareInitializer(context.Background(), mgr.opts, nodeID))
	require.NoError(t, mgr.StartSession(context.Background(), nodeID))

	// Verify status.
	status = mgr.Status()
	require.Equal(t, PostSetupStateComplete, status.State)
	require.Equal(t, uint64(mgr.opts.NumUnits)*mgr.cfg.LabelsPerUnit, status.NumLabelsWritten)
}

func TestPostSetupManager_findCommitmentAtx_UsesLatestAtx(t *testing.T) {
	mgr := newTestPostManager(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx := &types.ActivationTx{
		PublishEpoch: 1,
		NumUnits:     2,
		Weight:       2,
		SmesherID:    signer.NodeID(),
		TickCount:    1,
	}
	atx.SetID(types.RandomATXID())
	atx.SetReceived(time.Now())
	require.NoError(t, atxs.Add(mgr.db, atx, types.AtxBlob{}))
	mgr.atxsdata.AddFromAtx(atx, false)

	commitmentAtx, err := mgr.findCommitmentAtx(context.Background())
	require.NoError(t, err)
	require.Equal(t, atx.ID(), commitmentAtx)
}

func TestPostSetupManager_findCommitmentAtx_DefaultsToGoldenAtx(t *testing.T) {
	mgr := newTestPostManager(t)

	atx, err := mgr.findCommitmentAtx(context.Background())
	require.NoError(t, err)
	require.Equal(t, mgr.goldenATXID, atx)
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromPostMetadata(t *testing.T) {
	mgr := newTestPostManager(t)
	nodeID := types.RandomNodeID()

	// write commitment atx to metadata
	commitmentAtx := types.RandomATXID()
	err := initialization.SaveMetadata(mgr.opts.DataDir, &shared.PostMetadata{
		CommitmentAtxId: commitmentAtx.Bytes(),
		NodeId:          nodeID.Bytes(),
	})
	require.NoError(t, err)

	atxid, err := mgr.commitmentAtx(context.Background(), mgr.opts.DataDir, nodeID)
	require.NoError(t, err)
	require.NotNil(t, atxid)
	require.Equal(t, commitmentAtx, atxid)
}

func TestPostSetupManager_getCommitmentAtx_getsCommitmentAtxFromInitialAtx(t *testing.T) {
	mgr := newTestPostManager(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	// add an atx by the same node
	commitmentAtx := types.RandomATXID()
	atx := &types.ActivationTx{
		NumUnits:      1,
		Weight:        1,
		SmesherID:     signer.NodeID(),
		TickCount:     1,
		CommitmentATX: &commitmentAtx,
	}

	atx.SetID(types.RandomATXID())
	atx.SetReceived(time.Now())
	require.NoError(t, atxs.Add(mgr.cdb, atx, types.AtxBlob{}))

	atxid, err := mgr.commitmentAtx(context.Background(), mgr.opts.DataDir, signer.NodeID())
	require.NoError(t, err)
	require.Equal(t, commitmentAtx, atxid)
}

type testPostManager struct {
	*PostSetupManager

	opts PostSetupOpts

	cdb *datastore.CachedDB
}

func newTestPostManager(tb testing.TB) *testPostManager {
	tb.Helper()

	opts := DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	goldenATXID := types.ATXID{2, 3, 4}

	validator := NewMocknipostValidator(gomock.NewController(tb))
	validator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()
	validator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	syncer := NewMocksyncer(gomock.NewController(tb))
	synced := make(chan struct{})
	close(synced)
	syncer.EXPECT().RegisterForATXSynced().AnyTimes().Return(synced)

	logger := zaptest.NewLogger(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), logger)
	mgr, err := NewPostSetupManager(DefaultPostConfig(), logger, cdb, atxsdata.New(), goldenATXID, syncer, validator)
	require.NoError(tb, err)

	return &testPostManager{
		PostSetupManager: mgr,
		opts:             opts,
		cdb:              cdb,
	}
}
