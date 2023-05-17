package grpcserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

const (
	snapshot uint32 = 11
	restore  uint32 = 13
)

func TestAdminService_Checkpoint(t *testing.T) {
	logtest.SetupGlobal(t)
	ctrl := gomock.NewController(t)
	runner := NewMockCheckpointRunner(ctrl)
	mockFunc := func() CheckpointRunner {
		return runner
	}
	svc := NewAdminService(mockFunc)
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewAdminServiceClient(conn)

	fname := filepath.Join(t.TempDir(), "checkpoint")
	require.NoError(t, os.WriteFile(fname, types.RandomBytes(2*chunksize+1), 0x777))
	runner.EXPECT().Generate(gomock.Any(), types.LayerID(snapshot), types.LayerID(restore)).Return(fname, nil)

	stream, err := c.CheckpointStream(ctx, &pb.CheckpointStreamRequest{SnapshotLayer: snapshot, RestoreLayer: restore})
	require.NoError(t, err)

	receive := func(numRead ...int) {
		for _, num := range numRead {
			msg, err := stream.Recv()
			require.NoError(t, err)
			require.Len(t, msg.Data, num)
		}
	}
	receive(chunksize, chunksize, 1)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}

func TestAdminService_CheckpointError(t *testing.T) {
	logtest.SetupGlobal(t)
	ctrl := gomock.NewController(t)
	runner := NewMockCheckpointRunner(ctrl)
	mockFunc := func() CheckpointRunner {
		return runner
	}
	svc := NewAdminService(mockFunc)
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewAdminServiceClient(conn)

	gerr := errors.New("disaster")
	runner.EXPECT().Generate(gomock.Any(), types.LayerID(snapshot), types.LayerID(restore)).Return("", gerr)
	stream, err := c.CheckpointStream(ctx, &pb.CheckpointStreamRequest{SnapshotLayer: snapshot, RestoreLayer: restore})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, gerr.Error())
}
