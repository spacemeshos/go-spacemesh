package grpcserver

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// MeshService is a grpc server providing the MeshService
type MeshService struct {
	Network     api.NetworkAPI // P2P Swarm
	Tx          api.TxAPI      // Mesh
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer
}

func (s MeshService) registerService(server *grpc.Server) {
	pb.RegisterMeshServiceServer(server, s)
}

// NewMeshService creates a new grpc service using config data.
func NewMeshService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer) *MeshService {
	return &MeshService{
		Network:     net,
		Tx:          tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpc_server.MeshService")),
		Syncer:      syncer,
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
	return nil, nil
}

// CurrentEpoch returns the current epoch number
func (s MeshService) CurrentEpoch(ctx context.Context, in *pb.CurrentEpochRequest) (*pb.CurrentEpochResponse, error) {
	log.Info("GRPC MeshService.CurrentEpoch")
	return nil, nil
}

// NetID returns the network ID
func (s MeshService) NetID(ctx context.Context, in *pb.NetIDRequest) (*pb.NetIDResponse, error) {
	log.Info("GRPC MeshService.NetId")
	return nil, nil
}

// EpochNumLayers returns the number of layers per epoch (a network parameter)
func (s MeshService) EpochNumLayers(ctx context.Context, in *pb.EpochNumLayersRequest) (*pb.EpochNumLayersResponse, error) {
	log.Info("GRPC MeshService.EpochNumLayers")
	return nil, nil
}

// LayerDuration returns the layer duration in seconds (a network parameter)
func (s MeshService) LayerDuration(ctx context.Context, in *pb.LayerDurationRequest) (*pb.LayerDurationResponse, error) {
	log.Info("GRPC MeshService.LayerDuration")
	return nil, nil
}

// MaxTransactionsPerSecond returns the max number of tx per sec (a network parameter)
func (s MeshService) MaxTransactionsPerSecond(ctx context.Context, in *pb.MaxTransactionsPerSecondRequest) (*pb.MaxTransactionsPerSecondResponse, error) {
	log.Info("GRPC MeshService.MaxTransactionsPerSecond")
	return nil, nil
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
	return nil, nil
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
