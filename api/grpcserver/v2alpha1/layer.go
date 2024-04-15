package v2alpha1

import (
	"bytes"
	"context"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	Layer       = "layer_v2alpha1"
	LayerStream = "layer_stream_v2alpha1"
)

// layerMeshAPI is an api for getting mesh status.
type layerMeshAPI interface {
	LatestLayerInState() types.LayerID
	ProcessedLayer() types.LayerID
	GetLayerVerified(types.LayerID) (*types.Block, error)
}

func NewLayerStreamService(db sql.Executor) *LayerStreamService {
	return &LayerStreamService{db: db}
}

type LayerStreamService struct {
	db   sql.Executor
	mesh layerMeshAPI
}

func (s *LayerStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterLayerStreamServiceServer(server, s)
}

func (s *LayerStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterLayerStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *LayerStreamService) Stream(
	request *spacemeshv2alpha1.LayerStreamRequest,
	stream spacemeshv2alpha1.LayerStreamService_StreamServer,
) error {

	return nil
}

func (s *LayerStreamService) String() string {
	return "LayerStreamService"
}

func NewLayerService(db sql.Executor, msh layerMeshAPI) *LayerService {
	return &LayerService{
		db:   db,
		mesh: msh,
	}
}

type LayerService struct {
	db   sql.Executor
	mesh layerMeshAPI
}

func (s *LayerService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterLayerServiceServer(server, s)
}

func (s *LayerService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterLayerServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *LayerService) String() string {
	return "LayerService"
}

func (s *LayerService) List(
	ctx context.Context,
	request *spacemeshv2alpha1.LayerRequest,
) (*spacemeshv2alpha1.LayerList, error) {
	ops, err := toLayerOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	switch {
	case request.Limit > 1000:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 1000")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 1000")
	}

	lastLayerPassedHare := s.mesh.LatestLayerInState()
	lastLayerPassedTortoise := s.mesh.ProcessedLayer()
	rst := make([]*spacemeshv2alpha1.Layer, 0, request.Limit)
	var derr error
	if err := layers.IterateLayersOps(s.db, ops, func(layer *layers.Layer) bool {
		layerStatus := spacemeshv2alpha1.LayerV1_LAYER_STATUS_UNSPECIFIED
		if !layer.Id.After(lastLayerPassedHare) {
			layerStatus = spacemeshv2alpha1.LayerV1_LAYER_STATUS_APPLIED
		}
		if !layer.Id.After(lastLayerPassedTortoise) {
			layerStatus = spacemeshv2alpha1.LayerV1_LAYER_STATUS_VERIFIED
		}

		block, err := s.mesh.GetLayerVerified(layer.Id)
		if err != nil {
			ctxzap.Error(ctx, "could not read layer from database", layer.Id.Field().Zap(), zap.Error(err))
			derr = status.Errorf(codes.Internal, "error reading layer data: %v", err)
			return false
		}

		rst = append(rst, &spacemeshv2alpha1.Layer{Versioned: &spacemeshv2alpha1.Layer_V1{
			V1: toLayer(layer, layerStatus, block),
		}})

		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if derr != nil {
		return nil, derr
	}

	return &spacemeshv2alpha1.LayerList{Layers: rst}, nil
}

func toLayerOperations(filter *spacemeshv2alpha1.LayerRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}

	if filter.StartLayer != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Id,
			Token: builder.Gte,
			Value: int64(filter.StartLayer),
		})
	}

	if filter.EndLayer != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Id,
			Token: builder.Lte,
			Value: int64(filter.EndLayer),
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "id asc",
	})

	if filter.Limit != 0 {
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.Limit,
			Value: int64(filter.Limit),
		})
	}
	if filter.Offset != 0 {
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.Offset,
			Value: int64(filter.Offset),
		})
	}

	return ops, nil
}

func toLayer(layer *layers.Layer, layerStatus spacemeshv2alpha1.LayerV1_LayerStatus, block *types.Block) *spacemeshv2alpha1.LayerV1 {
	v1 := &spacemeshv2alpha1.LayerV1{
		Number: layer.Id.Uint32(),
		Status: layerStatus,
	}

	if bytes.Compare(layer.AggregatedHash.Bytes(), types.Hash32{}.Bytes()) != 0 {
		v1.ConsensusHash = layer.AggregatedHash.ShortString()
		v1.CumulativeStateHash = layer.AggregatedHash.Bytes()
	}

	if bytes.Compare(layer.StateHash.Bytes(), types.Hash32{}.Bytes()) != 0 {
		v1.StateHash = layer.StateHash.Bytes()
	}

	if block != nil {
		v1.Blocks = &spacemeshv2alpha1.Block{
			Versioned: &spacemeshv2alpha1.Block_V1{
				V1: &spacemeshv2alpha1.BlockV1{
					Id: types.Hash20(block.ID()).Bytes(),
				},
			},
		}
	}

	return v1
}
