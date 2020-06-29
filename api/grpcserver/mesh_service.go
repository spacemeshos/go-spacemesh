package grpcserver

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MeshService is a grpc server providing the MeshService
type MeshService struct {
	Network          api.NetworkAPI // P2P Swarm
	Mesh             api.TxAPI      // Mesh
	GenTime          api.GenesisTimeAPI
	PeerCounter      api.PeerCounter
	Syncer           api.Syncer
	LayersPerEpoch   int
	NetworkID        int8
	LayerDurationSec int
	LayerAvgSize     int
	TxsPerBlock      int
}

// RegisterService registers this service with a grpc server instance
func (s MeshService) RegisterService(server *Server) {
	pb.RegisterMeshServiceServer(server.GrpcServer, s)
}

// NewMeshService creates a new grpc service using config data.
func NewMeshService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer, layersPerEpoch int, networkId int8, layerDurationSec int,
	layerAvgSize int, txsPerBlock int) *MeshService {
	return &MeshService{
		Network:          net,
		Mesh:             tx,
		GenTime:          genTime,
		PeerCounter:      peers.NewPeers(net, log.NewDefault("grpc_server.MeshService")),
		Syncer:           syncer,
		LayersPerEpoch:   layersPerEpoch,
		NetworkID:        networkId,
		LayerDurationSec: layerDurationSec,
		LayerAvgSize:     layerAvgSize,
		TxsPerBlock:      txsPerBlock,
	}
}

// GenesisTime returns the network genesis time as UNIX time
func (s MeshService) GenesisTime(ctx context.Context, in *pb.GenesisTimeRequest) (*pb.GenesisTimeResponse, error) {
	log.Info("GRPC MeshService.GenesisTime")
	return &pb.GenesisTimeResponse{Unixtime: &pb.SimpleInt{
		Value: uint64(s.GenTime.GetGenesisTime().Unix()),
	}}, nil
}

// CurrentLayer returns the current layer number
func (s MeshService) CurrentLayer(ctx context.Context, in *pb.CurrentLayerRequest) (*pb.CurrentLayerResponse, error) {
	log.Info("GRPC MeshService.CurrentLayer")
	return &pb.CurrentLayerResponse{Layernum: &pb.SimpleInt{
		Value: s.GenTime.GetCurrentLayer().Uint64(),
	}}, nil
}

// CurrentEpoch returns the current epoch number
func (s MeshService) CurrentEpoch(ctx context.Context, in *pb.CurrentEpochRequest) (*pb.CurrentEpochResponse, error) {
	log.Info("GRPC MeshService.CurrentEpoch")
	curLayer := s.GenTime.GetCurrentLayer()
	return &pb.CurrentEpochResponse{Epochnum: &pb.SimpleInt{
		Value: uint64(curLayer.GetEpoch(uint16(s.LayersPerEpoch))),
	}}, nil
}

// NetID returns the network ID
func (s MeshService) NetID(ctx context.Context, in *pb.NetIDRequest) (*pb.NetIDResponse, error) {
	log.Info("GRPC MeshService.NetId")
	return &pb.NetIDResponse{Netid: &pb.SimpleInt{
		Value: uint64(s.NetworkID),
	}}, nil
}

// EpochNumLayers returns the number of layers per epoch (a network parameter)
func (s MeshService) EpochNumLayers(ctx context.Context, in *pb.EpochNumLayersRequest) (*pb.EpochNumLayersResponse, error) {
	log.Info("GRPC MeshService.EpochNumLayers")
	return &pb.EpochNumLayersResponse{Numlayers: &pb.SimpleInt{
		Value: uint64(s.LayersPerEpoch),
	}}, nil
}

// LayerDuration returns the layer duration in seconds (a network parameter)
func (s MeshService) LayerDuration(ctx context.Context, in *pb.LayerDurationRequest) (*pb.LayerDurationResponse, error) {
	log.Info("GRPC MeshService.LayerDuration")
	return &pb.LayerDurationResponse{Duration: &pb.SimpleInt{
		Value: uint64(s.LayerDurationSec),
	}}, nil
}

// MaxTransactionsPerSecond returns the max number of tx per sec (a network parameter)
func (s MeshService) MaxTransactionsPerSecond(ctx context.Context, in *pb.MaxTransactionsPerSecondRequest) (*pb.MaxTransactionsPerSecondResponse, error) {
	log.Info("GRPC MeshService.MaxTransactionsPerSecond")
	return &pb.MaxTransactionsPerSecondResponse{Maxtxpersecond: &pb.SimpleInt{
		Value: uint64(s.TxsPerBlock * s.LayerAvgSize / s.LayerDurationSec),
	}}, nil
}

// QUERIES

// AccountMeshDataQuery returns account data
func (s MeshService) AccountMeshDataQuery(ctx context.Context, in *pb.AccountMeshDataQueryRequest) (*pb.AccountMeshDataQueryResponse, error) {
	log.Info("GRPC MeshService.AccountMeshDataQuery")
	return nil, nil
}

// LayersQuery returns all mesh data, layer by layer
func (s MeshService) LayersQuery(ctx context.Context, in *pb.LayersQueryRequest) (*pb.LayersQueryResponse, error) {
	log.Info("GRPC MeshService.LayersQuery")

	// current layer (based on time)
	currentLayer := s.GenTime.GetCurrentLayer().Uint64()

	// last validated layer
	// TODO: don't know if this is tortoise or hare, or how to tell
	//lastValidLayer := s.Mesh.ProcessedLayer()
	lastValidLayer := s.Mesh.LatestLayerInState()

	// Validate inputs
	if in.StartLayer < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "`StartLayer` must be greater than zero")
	}
	//if uint64(in.StartLayer) > currentLayer {
	//	return nil, status.Errorf(codes.InvalidArgument, "`StartLayer` must be less than or equal to current layer")
	//}
	if in.StartLayer > in.EndLayer {
		return nil, status.Errorf(codes.InvalidArgument, "`StartLayer` must not be greater than `EndLayer`")
	}

	layers := make([]*pb.Layer, in.EndLayer-in.StartLayer)
	layerStatus := pb.Layer_LAYER_STATUS_UNSPECIFIED
	for l := uint64(in.StartLayer); l <= uint64(in.EndLayer); l++ {
		// TODO: only run this once
		if l >= lastValidLayer.Uint64() {
			layerStatus = pb.Layer_LAYER_STATUS_CONFIRMED
		}

		layer, err := s.Mesh.GetLayer(types.LayerID(l))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error retreiving layer data")
		}

		// Load all block data from layer.Blocks()

		// Extract ATX data from block data

		layers = append(layers, &pb.Layer{
			Number:        l,
			Status:        layerStatus,
			Hash:          nil, // do we need this?
			Blocks:        nil,
			Activations:   nil,
			RootStateHash: nil, // do we need this?
		})
	}
	return &pb.LayersQueryResponse{Layer: layers}, nil
}

// STREAMS

// AccountMeshDataStream returns a stream of transactions and activations for an account
func (s MeshService) AccountMeshDataStream(request *pb.AccountMeshDataStreamRequest, stream pb.MeshService_AccountMeshDataStreamServer) error {
	log.Info("GRPC MeshService.AccountMeshDataStream")
	return nil
}

// LayerStream returns a stream of all mesh data per layer
func (s MeshService) LayerStream(request *pb.LayerStreamRequest, stream pb.MeshService_LayerStreamServer) error {
	log.Info("GRPC MeshService.LayerStream")
	return nil
}
