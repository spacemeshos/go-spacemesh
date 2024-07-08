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
	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const snapshot uint32 = 15

func newAtx(tb testing.TB, db *sql.Database) {
	atx := &types.ActivationTx{
		PublishEpoch:   types.EpochID(2),
		Sequence:       0,
		CommitmentATX:  &types.ATXID{1},
		NumUnits:       2,
		Coinbase:       types.Address{1, 2, 3},
		BaseTickHeight: 1111,
		TickCount:      12,
		VRFNonce:       types.VRFPostIndex(11),
	}
	atx.SetID(types.RandomATXID())
	atx.SmesherID = types.BytesToNodeID(types.RandomBytes(20))
	atx.SetReceived(time.Now().Local())
	require.NoError(tb, atxs.Add(db, atx, types.AtxBlob{}))
	require.NoError(tb, atxs.SetUnits(db, atx.ID(), atx.SmesherID, atx.NumUnits))
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
				Kind:     peerinfo.KindOutbound,
			},
		},
		Tags: []string{"t1", "t2"},
	})
	p.EXPECT().ConnectedPeerInfo(p2).Return(&p2p.PeerInfo{
		ID: p2,
		DataStats: p2p.DataStats{
			BytesSent:     100,
			BytesReceived: 300,
			SendRate:      [2]int64{100, 10},
			RecvRate:      [2]int64{300, 30},
		},
		ClientStats: p2p.PeerRequestStats{
			SuccessCount: 1,
			FailureCount: 0,
			Latency:      100 * time.Millisecond,
		},
		ServerStats: p2p.PeerRequestStats{
			SuccessCount: 10,
			FailureCount: 2,
			Latency:      50 * time.Millisecond,
		},
		Connections: []p2p.ConnectionInfo{
			{
				Address:  mustParseMultiaddr("/ip4/10.11.22.33/tcp/5111"),
				Uptime:   30 * time.Second,
				Outbound: true,
				Kind:     peerinfo.KindHolePunchOutbound,
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
	require.Nil(t, msg.ClientStats)
	require.Nil(t, msg.ServerStats)
	require.Equal(t, []string{"t1", "t2"}, msg.Tags)

	msg, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, p2.String(), msg.Id)
	require.Len(t, msg.Connections, 1)
	require.Equal(t, "/ip4/10.11.22.33/tcp/5111", msg.Connections[0].Address)
	require.Equal(t, 30*time.Second, msg.Connections[0].Uptime.AsDuration())
	require.True(t, msg.Connections[0].Outbound)
	require.Equal(t, pb.ConnectionInfo_HPOutbound, msg.Connections[0].Kind)
	require.Equal(t, uint64(100), msg.BytesSent)
	require.Equal(t, uint64(300), msg.BytesReceived)
	require.Equal(t, uint64(100), msg.SendRate[0])
	require.Equal(t, uint64(300), msg.RecvRate[0])
	require.Equal(t, uint64(10), msg.SendRate[1])
	require.Equal(t, uint64(30), msg.RecvRate[1])
	require.NotNil(t, msg.ClientStats)
	require.Equal(t, uint64(1), msg.ClientStats.SuccessCount)
	require.Equal(t, uint64(0), msg.ClientStats.FailureCount)
	require.Equal(t, 100*time.Millisecond, msg.ClientStats.Latency.AsDuration())
	require.NotNil(t, msg.ServerStats)
	require.Equal(t, uint64(10), msg.ServerStats.SuccessCount)
	require.Equal(t, uint64(2), msg.ServerStats.FailureCount)
	require.Equal(t, 50*time.Millisecond, msg.ServerStats.Latency.AsDuration())
	require.Equal(t, []string{"t3"}, msg.Tags)

	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}
