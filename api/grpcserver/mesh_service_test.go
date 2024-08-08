package grpcserver

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const (
	epoch = 7
	layer = 123
)

func AtxMalfeasance(tb testing.TB, db sql.Executor) (types.NodeID, *wire.MalfeasanceProof) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	ap := wire.AtxProof{
		Messages: [2]wire.AtxProofMsg{
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
	mp := &wire.MalfeasanceProof{
		Layer: types.LayerID(layer),
		Proof: wire.Proof{
			Type: wire.MultipleATXs,
			Data: &ap,
		},
	}
	data, err := codec.Encode(mp)
	require.NoError(tb, err)
	require.NoError(tb, identities.SetMalicious(db, sig.NodeID(), data, time.Now()))
	return sig.NodeID(), mp
}

func BallotMalfeasance(tb testing.TB, db sql.Executor) (types.NodeID, *wire.MalfeasanceProof) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	bp := wire.BallotProof{
		Messages: [2]wire.BallotProofMsg{
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
	mp := &wire.MalfeasanceProof{
		Layer: types.LayerID(layer),
		Proof: wire.Proof{
			Type: wire.MultipleBallots,
			Data: &bp,
		},
	}
	data, err := codec.Encode(mp)
	require.NoError(tb, err)
	require.NoError(tb, identities.SetMalicious(db, sig.NodeID(), data, time.Now()))
	return sig.NodeID(), mp
}

func HareMalfeasance(tb testing.TB, db sql.Executor) (types.NodeID, *wire.MalfeasanceProof) {
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	hp := wire.HareProof{
		Messages: [2]wire.HareProofMsg{
			{
				InnerMsg: wire.HareMetadata{
					Layer:   types.LayerID(layer),
					Round:   3,
					MsgHash: types.RandomHash(),
				},
			},
			{
				InnerMsg: wire.HareMetadata{
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
	mp := &wire.MalfeasanceProof{
		Layer: types.LayerID(layer),
		Proof: wire.Proof{
			Type: wire.HareEquivocation,
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
	srv := NewMeshService(
		datastore.NewCachedDB(db, zaptest.NewLogger(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, cleanup := launchServer(t, srv)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := pb.NewMeshServiceClient(conn)
	nodeID, proof := BallotMalfeasance(t, db)

	req := &pb.MalfeasanceRequest{
		SmesherHex:   "0123456789abcdef",
		IncludeProof: true,
	}
	resp, err := client.MalfeasanceQuery(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Nil(t, resp)

	req.SmesherHex = hex.EncodeToString(nodeID.Bytes())
	resp, err = client.MalfeasanceQuery(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, nodeID, types.BytesToNodeID(resp.Proof.SmesherId.Id))
	require.EqualValues(t, layer, resp.Proof.Layer.Number)
	require.Equal(t, pb.MalfeasanceProof_MALFEASANCE_BALLOT, resp.Proof.Kind)
	require.Equal(t, events.ToMalfeasancePB(nodeID, proof, true), resp.Proof)
	require.NotEmpty(t, resp.Proof.Proof)
	var got wire.MalfeasanceProof
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
	srv := NewMeshService(
		datastore.NewCachedDB(db, zaptest.NewLogger(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, cleanup := launchServer(t, srv)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(t, cfg)
	client := pb.NewMeshServiceClient(conn)

	for range 10 {
		AtxMalfeasance(t, db)
		BallotMalfeasance(t, db)
		HareMalfeasance(t, db)
	}

	stream, err := client.MalfeasanceStream(ctx, &pb.MalfeasanceStreamRequest{})
	require.NoError(t, err)
	_, err = stream.Header()
	require.NoError(t, err)

	var total, atx, ballot, hare int
	for range 30 {
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
		require.EqualValues(t, layer, got.Layer.Number)
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

type MeshAPIMockInstrumented struct {
	MeshAPIMock
}

var (
	instrumentedErr         error
	instrumentedBlock       *types.Block
	instrumentedMissing     bool
	instrumentedNoStateRoot bool
)

func (m *MeshAPIMockInstrumented) GetLayerVerified(tid types.LayerID) (*types.Block, error) {
	return instrumentedBlock, instrumentedErr
}

type ConStateAPIMockInstrumented struct {
	ConStateAPIMock
}

func (t *ConStateAPIMockInstrumented) GetMeshTransactions(
	txIds []types.TransactionID,
) (txs []*types.MeshTransaction, missing map[types.TransactionID]struct{}) {
	txs, missing = t.ConStateAPIMock.GetMeshTransactions(txIds)
	if instrumentedMissing {
		// arbitrarily return one missing tx
		missing = map[types.TransactionID]struct{}{
			txs[0].ID: {},
		}
	}
	return
}

func (t *ConStateAPIMockInstrumented) GetLayerStateRoot(types.LayerID) (types.Hash32, error) {
	if instrumentedNoStateRoot {
		return stateRoot, errors.New("error")
	}
	return stateRoot, nil
}

func TestReadLayer(t *testing.T) {
	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	db := sql.InMemory()
	srv := NewMeshService(
		datastore.NewCachedDB(db, zaptest.NewLogger(t)),
		&MeshAPIMockInstrumented{},
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	instrumentedErr = errors.New("error")
	_, err := srv.readLayer(ctx, layer, pb.Layer_LAYER_STATUS_UNSPECIFIED)
	require.ErrorContains(t, err, "error reading layer data")

	instrumentedErr = nil
	_, err = srv.readLayer(ctx, layer, pb.Layer_LAYER_STATUS_UNSPECIFIED)
	require.NoError(t, err)

	srv = NewMeshService(
		datastore.NewCachedDB(db, zaptest.NewLogger(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	_, err = srv.readLayer(ctx, layer, pb.Layer_LAYER_STATUS_UNSPECIFIED)
	require.NoError(t, err)

	// now instrument conStateAPI to return errors
	srv = NewMeshService(
		datastore.NewCachedDB(db, zaptest.NewLogger(t)),
		meshAPIMock,
		&ConStateAPIMockInstrumented{*conStateAPI},
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	instrumentedMissing = true
	_, err = srv.readLayer(ctx, layer, pb.Layer_LAYER_STATUS_UNSPECIFIED)
	require.ErrorContains(t, err, "error retrieving tx data")

	instrumentedMissing = false
	instrumentedNoStateRoot = true
	_, err = srv.readLayer(ctx, layer, pb.Layer_LAYER_STATUS_UNSPECIFIED)
	require.NoError(t, err)
}
