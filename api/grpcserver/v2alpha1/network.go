package v2alpha1

import (
	"context"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	Network = "network_v2alpha1"
)

func NewNetworkService(
	genTime genesisTimeAPI,
	genesisID types.Hash20,
	layerDuration time.Duration,
) *NetworkService {
	return &NetworkService{
		genTime:       genTime,
		genesisID:     genesisID,
		layerDuration: layerDuration,
	}
}

type NetworkService struct {
	genTime       genesisTimeAPI
	genesisID     types.Hash20
	layerDuration time.Duration
}

func (s *NetworkService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterNetworkServiceServer(server, s)
}

func (s *NetworkService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterNetworkServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *NetworkService) String() string {
	return "NetworkService"
}

func (s *NetworkService) Info(context.Context, *emptypb.Empty) (*spacemeshv2alpha1.NetworkInfoResponse, error) {
	return &spacemeshv2alpha1.NetworkInfoResponse{
		GenesisTime:           uint64(s.genTime.GenesisTime().Unix()),
		LayerDuration:         uint32(s.layerDuration.Seconds()),
		GenesisId:             s.genesisID.Bytes(),
		Hrp:                   types.NetworkHRP(),
		EffectiveGenesisLayer: types.GetEffectiveGenesis().Uint32(),
		LayersPerEpoch:        types.GetLayersPerEpoch(),
	}, nil
}
