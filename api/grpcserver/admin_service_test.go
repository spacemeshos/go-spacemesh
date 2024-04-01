package grpcserver

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/conninfo"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const snapshot uint32 = 15

func newAtx(tb testing.TB, db *sql.Database) {
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
	vrfNonce := types.VRFPostIndex(11)
	atx.VRFNonce = &vrfNonce
	atx.SmesherID = types.BytesToNodeID(types.RandomBytes(20))
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	vatx, err := atx.Verify(1111, 12)
	require.NoError(tb, err)
	require.NoError(tb, atxs.Add(db, vatx))
}

func createMesh(tb testing.TB, db *sql.Database) {
	for range 10 {
		newAtx(tb, db)
	}
	acct := &types.Account{
		Layer:           types.LayerID(0),
		Address:         types.Address{1, 1},
		NextNonce:       1,
		Balance:         1300,
		TemplateAddress: &types.Address{2},
		State:           []byte("state10"),
	}
	require.NoError(tb, accounts.Update(db, acct))
}

func TestAdminService_Checkpoint(t *testing.T) {
	db := sql.InMemory()
	createMesh(t, db)
	svc := NewAdminService(db, t.TempDir(), nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
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
	db := sql.InMemory()
	svc := NewAdminService(db, t.TempDir(), nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewAdminServiceClient(conn)

	stream, err := c.CheckpointStream(ctx, &pb.CheckpointStreamRequest{SnapshotLayer: snapshot})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, sql.ErrNotFound.Error())
}

func TestAdminService_Recovery(t *testing.T) {
	db := sql.InMemory()
	recoveryCalled := atomic.Bool{}
	svc := NewAdminService(db, t.TempDir(), nil)
	svc.recover = func() { recoveryCalled.Store(true) }

	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewAdminServiceClient(conn)

	_, err := c.Recover(ctx, &pb.RecoverRequest{})
	require.NoError(t, err)
	require.True(t, recoveryCalled.Load())
}

func TestAdminService_PeerInfo(t *testing.T) {
	ctrl := gomock.NewController(t)
	p := NewMockpeers(ctrl)

	db := sql.InMemory()
	svc := NewAdminService(db, t.TempDir(), p)

	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewAdminServiceClient(conn)

	p1 := p2p.Peer("p1")
	p2 := p2p.Peer("p2")
	p.EXPECT().GetPeers().Return([]p2p.Peer{p1, p2})
	p.EXPECT().ConnectedPeerInfo(p1).Return(&p2p.PeerInfo{
		ID: p1,
		Connections: []p2p.ConnectionInfo{
			{
				Address:  mustParseMultiaddr("/ip4/10.36.0.221/tcp/5000"),
				Uptime:   100 * time.Second,
				Outbound: true,
				Kind:     conninfo.KindOutbound,
			},
		},
		Tags: []string{"t1", "t2"},
	})
	p.EXPECT().ConnectedPeerInfo(p2).Return(&p2p.PeerInfo{
		ID: p2,
		Connections: []p2p.ConnectionInfo{
			{
				Address:  mustParseMultiaddr("/ip4/10.11.22.33/tcp/5111"),
				Uptime:   30 * time.Second,
				Outbound: true,
				Kind:     conninfo.KindHolePunchOutbound,
				ClientConnStats: p2p.PeerConnectionStats{
					SuccessCount:  1,
					FailureCount:  0,
					Latency:       100 * time.Millisecond,
					BytesSent:     100,
					BytesReceived: 300,
				},
				ServerConnStats: p2p.PeerConnectionStats{
					BytesSent:     600,
					BytesReceived: 50,
				},
			},
		},
		Tags: []string{"t3"},
	})

	stream, err := c.PeerInfoStream(ctx, &emptypb.Empty{})
	require.NoError(t, err)

	msg, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, p1.String(), msg.Id)
	require.Len(t, msg.Connections, 1)
	require.Equal(t, "/ip4/10.36.0.221/tcp/5000", msg.Connections[0].Address)
	require.Equal(t, 100*time.Second, msg.Connections[0].Uptime.AsDuration())
	require.True(t, msg.Connections[0].Outbound)
	require.Equal(t, pb.ConnectionInfo_Outbound, msg.Connections[0].Kind)
	require.Nil(t, msg.Connections[0].ClientStats)
	require.Nil(t, msg.Connections[0].ServerStats)
	require.Equal(t, []string{"t1", "t2"}, msg.Tags)

	msg, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, p2.String(), msg.Id)
	require.Len(t, msg.Connections, 1)
	require.Equal(t, "/ip4/10.11.22.33/tcp/5111", msg.Connections[0].Address)
	require.Equal(t, 30*time.Second, msg.Connections[0].Uptime.AsDuration())
	require.True(t, msg.Connections[0].Outbound)
	require.Equal(t, pb.ConnectionInfo_HPOutbound, msg.Connections[0].Kind)
	require.NotNil(t, msg.Connections[0].ClientStats)
	require.Equal(t, uint64(1), msg.Connections[0].ClientStats.SuccessCount)
	require.Equal(t, uint64(0), msg.Connections[0].ClientStats.FailureCount)
	require.Equal(t, 100*time.Millisecond, msg.Connections[0].ClientStats.Latency.AsDuration())
	require.Equal(t, uint64(100), msg.Connections[0].ClientStats.BytesSent)
	require.Equal(t, uint64(300), msg.Connections[0].ClientStats.BytesReceived)
	require.NotNil(t, msg.Connections[0].ServerStats)
	require.Equal(t, uint64(0), msg.Connections[0].ServerStats.SuccessCount)
	require.Equal(t, uint64(0), msg.Connections[0].ServerStats.FailureCount)
	require.Nil(t, msg.Connections[0].ServerStats.Latency)
	require.Equal(t, uint64(600), msg.Connections[0].ServerStats.BytesSent)
	require.Equal(t, uint64(50), msg.Connections[0].ServerStats.BytesReceived)
	require.Equal(t, []string{"t3"}, msg.Tags)

	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}
