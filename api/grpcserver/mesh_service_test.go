package grpcserver

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const (
	epoch = 7
	layer = 123
)

func AtxMalfeasance(tb testing.TB, db sql.Executor) (types.NodeID, *types.MalfeasanceProof) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	ap := types.AtxProof{
		Messages: [2]types.AtxProofMsg{
			{
				InnerMsg: types.ATXMetadata{
					PublishEpoch: types.EpochID(epoch),
					MsgHash:      types.RandomHash(),
				},
			},
			{
				InnerMsg: types.ATXMetadata{
					PublishEpoch: types.EpochID(epoch),
					MsgHash:      types.RandomHash(),
				},
			},
		},
	}
	ap.Messages[0].Signature = sig.Sign(signing.ATX, ap.Messages[0].SignedBytes())
	ap.Messages[0].SmesherID = sig.NodeID()
	ap.Messages[1].Signature = sig.Sign(signing.ATX, ap.Messages[1].SignedBytes())
	ap.Messages[1].SmesherID = sig.NodeID()
	mp := &types.MalfeasanceProof{
		Layer: types.LayerID(layer),
		Proof: types.Proof{
			Type: types.MultipleATXs,
			Data: &ap,
		},
	}
	data, err := codec.Encode(mp)
	require.NoError(tb, err)
	require.NoError(tb, identities.SetMalicious(db, sig.NodeID(), data, time.Now()))
	return sig.NodeID(), mp
}

func BallotMalfeasance(tb testing.TB, db sql.Executor) (types.NodeID, *types.MalfeasanceProof) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	bp := types.BallotProof{
		Messages: [2]types.BallotProofMsg{
			{
				InnerMsg: types.BallotMetadata{
					Layer:   types.LayerID(layer),
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: types.BallotMetadata{
					Layer:   types.LayerID(layer),
					MsgHash: types.RandomHash(),
				},
			},
		},
	}
	bp.Messages[0].Signature = sig.Sign(signing.BALLOT, bp.Messages[0].SignedBytes())
	bp.Messages[0].SmesherID = sig.NodeID()
	bp.Messages[1].Signature = sig.Sign(signing.BALLOT, bp.Messages[1].SignedBytes())
	bp.Messages[1].SmesherID = sig.NodeID()
	mp := &types.MalfeasanceProof{
		Layer: types.LayerID(layer),
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &bp,
		},
	}
	data, err := codec.Encode(mp)
	require.NoError(tb, err)
	require.NoError(tb, identities.SetMalicious(db, sig.NodeID(), data, time.Now()))
	return sig.NodeID(), mp
}

func HareMalfeasance(tb testing.TB, db sql.Executor) (types.NodeID, *types.MalfeasanceProof) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	hp := types.HareProof{
		Messages: [2]types.HareProofMsg{
			{
				InnerMsg: types.HareMetadata{
					Layer:   types.LayerID(layer),
					Round:   3,
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: types.HareMetadata{
					Layer:   types.LayerID(layer),
					Round:   3,
					MsgHash: types.RandomHash(),
				},
			},
		},
	}
	hp.Messages[0].Signature = sig.Sign(signing.HARE, hp.Messages[0].SignedBytes())
	hp.Messages[0].SmesherID = sig.NodeID()
	hp.Messages[1].Signature = sig.Sign(signing.HARE, hp.Messages[1].SignedBytes())
	hp.Messages[1].SmesherID = sig.NodeID()
	mp := &types.MalfeasanceProof{
		Layer: types.LayerID(layer),
		Proof: types.Proof{
			Type: types.HareEquivocation,
			Data: &hp,
		},
	}
	data, err := codec.Encode(mp)
	require.NoError(tb, err)
	require.NoError(tb, identities.SetMalicious(db, sig.NodeID(), data, time.Now()))
	return sig.NodeID(), mp
}

func TestMeshService_MalfeasanceQuery(t *testing.T) {
	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	db := sql.InMemory()
	srv := NewMeshService(datastore.NewCachedDB(db, logtest.New(t)), meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	cfg, cleanup := launchServer(t, srv)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	client := pb.NewMeshServiceClient(conn)
	nodeID, proof := BallotMalfeasance(t, db)

	req := &pb.MalfeasanceRequest{
		SmesherHex:   "0123456789abcdef",
		IncludeProof: true,
	}
	resp, err := client.MalfeasanceQuery(context.Background(), req)
	require.Equal(t, status.Code(err), codes.InvalidArgument)
	require.Nil(t, resp)

	req.SmesherHex = hex.EncodeToString(nodeID.Bytes())
	resp, err = client.MalfeasanceQuery(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, nodeID, types.BytesToNodeID(resp.Proof.SmesherId.Id))
	require.EqualValues(t, layer, resp.Proof.Layer.Number)
	require.Equal(t, pb.MalfeasanceProof_MALFEASANCE_BALLOT, resp.Proof.Kind)
	require.Equal(t, events.ToMalfeasancePB(nodeID, proof, true), resp.Proof)
	require.NotEmpty(t, resp.Proof.Proof)
	var got types.MalfeasanceProof
	require.NoError(t, codec.Decode(resp.Proof.Proof, &got))
	require.Equal(t, *proof, got)

	req.IncludeProof = false
	resp, err = client.MalfeasanceQuery(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, resp.Proof.Proof)
}

func TestMeshService_MalfeasanceStream(t *testing.T) {
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	db := sql.InMemory()
	srv := NewMeshService(datastore.NewCachedDB(db, logtest.New(t)), meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	cfg, cleanup := launchServer(t, srv)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	client := pb.NewMeshServiceClient(conn)

	for i := 0; i < 10; i++ {
		AtxMalfeasance(t, db)
		BallotMalfeasance(t, db)
		HareMalfeasance(t, db)
	}

	stream, err := client.MalfeasanceStream(ctx, &pb.MalfeasanceStreamRequest{})
	require.NoError(t, err)
	_, err = stream.Header()
	require.NoError(t, err)

	var total, atx, ballot, hare int
	for i := 0; i < 30; i++ {
		resp, err := stream.Recv()
		require.NoError(t, err)
		total++
		got := resp.Proof
		switch got.Kind {
		case pb.MalfeasanceProof_MALFEASANCE_ATX:
			atx++
		case pb.MalfeasanceProof_MALFEASANCE_BALLOT:
			ballot++
		case pb.MalfeasanceProof_MALFEASANCE_HARE:
			hare++
		}
		require.EqualValues(t, got.Layer.Number, layer)
	}
	require.Equal(t, 30, total)
	require.Equal(t, 10, atx)
	require.Equal(t, 10, ballot)
	require.Equal(t, 10, hare)

	id, proof := AtxMalfeasance(t, db)
	events.ReportMalfeasance(id, proof)
	resp, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, events.ToMalfeasancePB(id, proof, false), resp.Proof)
	id, proof = BallotMalfeasance(t, db)
	events.ReportMalfeasance(id, proof)
	resp, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, events.ToMalfeasancePB(id, proof, false), resp.Proof)
}
