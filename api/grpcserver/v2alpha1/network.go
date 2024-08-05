package v2alpha1

import (
	"context"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	Network = "network_v2alpha1"
)

func NewNetworkService(genesisTime time.Time, genesisID types.Hash20, layerDuration time.Duration,
	labelsPerUnit uint64,
) *NetworkService {
	return &NetworkService{
		genesisTime:   genesisTime,
		genesisID:     genesisID,
		layerDuration: layerDuration,
		labelsPerUnit: labelsPerUnit,
	}
}

type NetworkService struct {
	genesisTime   time.Time
	genesisID     types.Hash20
	layerDuration time.Duration
	labelsPerUnit uint64
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

func (s *NetworkService) Info(context.Context,
	*spacemeshv2alpha1.NetworkInfoRequest,
) (*spacemeshv2alpha1.NetworkInfoResponse, error) {
	return &spacemeshv2alpha1.NetworkInfoResponse{
		GenesisTime:           timestamppb.New(s.genesisTime),
		LayerDuration:         durationpb.New(s.layerDuration),
		GenesisId:             s.genesisID.Bytes(),
		Hrp:                   types.NetworkHRP(),
		EffectiveGenesisLayer: types.GetEffectiveGenesis().Uint32(),
		LayersPerEpoch:        types.GetLayersPerEpoch(),
		LabelsPerUnit:         s.labelsPerUnit,
	}, nil
}
