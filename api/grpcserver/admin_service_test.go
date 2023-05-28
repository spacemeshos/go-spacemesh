package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const (
	snapshot uint32 = 15
	restore  uint32 = 17
)

func newatx(tb testing.TB, db *sql.Database) {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:  types.EpochID(2),
				Sequence:      0,
				CommitmentATX: &types.ATXID{1},
			},
			NumUnits: 2,
			Coinbase: types.Address{1, 2, 3},
		},
	}
	atx.SetID(types.RandomATXID())
	vrfnonce := types.VRFPostIndex(11)
	atx.VRFNonce = &vrfnonce
	atx.SmesherID = types.BytesToNodeID(types.RandomBytes(20))
	atx.NodeID = &atx.SmesherID
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	vatx, err := atx.Verify(1111, 12)
	require.NoError(tb, err)
	require.NoError(tb, atxs.Add(db, vatx))
}

func createMesh(tb testing.TB, db *sql.Database) {
	for i := 0; i < 10; i++ {
		newatx(tb, db)
	}
	acct := &types.Account{
		Layer: types.LayerID(0), Address: types.Address{1, 1}, NextNonce: 1, Balance: 1300, TemplateAddress: &types.Address{2}, State: []byte("state10"),
	}
	require.NoError(tb, accounts.Update(db, acct))
}

func TestAdminService_Checkpoint(t *testing.T) {
	logtest.SetupGlobal(t)
	db := sql.InMemory()
	createMesh(t, db)
	svc := NewAdminService(db, t.TempDir(), logtest.New(t))
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewAdminServiceClient(conn)

	stream, err := c.CheckpointStream(ctx, &pb.CheckpointStreamRequest{SnapshotLayer: snapshot})
	require.NoError(t, err)

	var chunks int
	read := func() {
		for {
			select {
			case <-ctx.Done():
				t.Fail()
			default:
				msg, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return
				}
				require.NoError(t, err)
				if len(msg.Data) == chunksize {
					chunks++
				}
			}
		}
	}
	read()
	require.NotZero(t, chunks)
}

func TestAdminService_CheckpointError(t *testing.T) {
	logtest.SetupGlobal(t)
	db := sql.InMemory()
	svc := NewAdminService(db, t.TempDir(), logtest.New(t))
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewAdminServiceClient(conn)

	stream, err := c.CheckpointStream(ctx, &pb.CheckpointStreamRequest{SnapshotLayer: snapshot})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, sql.ErrNotFound.Error())
}

func TestAdminService_RecoveryFileMissing(t *testing.T) {
	logtest.SetupGlobal(t)
	db := sql.InMemory()
	svc := NewAdminService(db, t.TempDir(), logtest.New(t))
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewAdminServiceClient(conn)

	fname := filepath.Join(t.TempDir(), "snapshot")
	_, err := c.Recover(ctx, &pb.RecoverRequest{Uri: fmt.Sprintf("file://%s", fname), RestoreLayer: restore})
	require.Error(t, err)
}
