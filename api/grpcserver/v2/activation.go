package v2

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	spacemeshv2 "github.com/spacemeshos/api/release/go/spacemesh/v2"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const (
	Activation       = "activation_v2"
	ActivationStream = "activation_stream_v2"
)

func NewActivationStreamService(db *sql.Database) *ActivationStreamService {
	return &ActivationStreamService{db: db}
}

type ActivationStreamService struct {
	db *sql.Database
}

var _ grpcserver.ServiceAPI = (*ActivationStreamService)(nil)

func (s *ActivationStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2.RegisterActivationStreamServiceServer(server, s)
}

func (s *ActivationStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2.RegisterActivationStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *ActivationStreamService) String() string {
	return "ActivationStreamService"
}

func (s *ActivationStreamService) Stream(
	request *spacemeshv2.ActivationStreamRequest,
	stream spacemeshv2.ActivationStreamService_StreamServer,
) error {
	if request.Watch {
		return status.Error(codes.InvalidArgument, "watch is not supported")
	}
	ops, err := toOperations(toRequest(request))
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var ierr error
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.VerifiedActivationTx) bool {
		ierr = stream.Send(&spacemeshv2.Activation{Versioned: &spacemeshv2.Activation_V1{V1: toAtx(atx)}})
		return ierr == nil
	}); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
}

func (s *ActivationStreamService) StreamHeaders(
	request *spacemeshv2.ActivationStreamRequest,
	stream spacemeshv2.ActivationStreamService_StreamHeadersServer,
) error {
	if request.Watch {
		return status.Error(codes.InvalidArgument, "watch is not supported")
	}
	ops, err := toOperations(toRequest(request))
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var ierr error
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.VerifiedActivationTx) bool {
		ierr = stream.Send(&spacemeshv2.ActivationHeader{Versioned: &spacemeshv2.ActivationHeader_V1{
			V1: toHeader(atx)}})
		return ierr == nil
	}); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
}

func toAtx(atx *types.VerifiedActivationTx) *spacemeshv2.ActivationV1 {
	return &spacemeshv2.ActivationV1{
		Id:             atx.ID().Bytes(),
		NodeId:         atx.SmesherID.Bytes(),
		Signature:      atx.Signature.Bytes(),
		PublishEpoch:   atx.PublishEpoch.Uint32(),
		Sequence:       atx.Sequence,
		PrevAtx:        atx.PrevATXID[:],
		PositioningAtx: atx.PositioningATX[:],
		Coinbase:       atx.Coinbase.String(),
		Units:          atx.NumUnits,
		BaseHeight:     uint32(atx.BaseTickHeight()),
		Ticks:          uint32(atx.TickCount()),
	}
}

func toHeader(atx *types.VerifiedActivationTx) *spacemeshv2.ActivationHeaderV1 {
	return &spacemeshv2.ActivationHeaderV1{
		Id:           atx.ID().Bytes(),
		NodeId:       atx.SmesherID.Bytes(),
		PublishEpoch: atx.PublishEpoch.Uint32(),
		Coinbase:     atx.Coinbase.String(),
		Units:        atx.NumUnits,
		BaseHeight:   uint32(atx.BaseTickHeight()),
		Ticks:        uint32(atx.TickCount()),
	}
}

func NewActivationService(db *sql.Database) *ActivationService {
	return &ActivationService{db: db}
}

type ActivationService struct {
	db *sql.Database
}

var _ grpcserver.ServiceAPI = (*ActivationService)(nil)

func (s *ActivationService) RegisterService(server *grpc.Server) {
	spacemeshv2.RegisterActivationServiceServer(server, s)
}

func (s *ActivationService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2.RegisterActivationServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *ActivationService) String() string {
	return "ActivationService"
}

func (s *ActivationService) List(
	ctx context.Context,
	request *spacemeshv2.ActivationRequest,
) (*spacemeshv2.ActivationList, error) {
	ops, err := toOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// every full atx is ~1KB. 100 atxs is ~100KB.
	if request.Limit > 100 {
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	} else if request.Limit == 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be set to a value below 100")
	}
	rst := make([]*spacemeshv2.Activation, 0, request.Limit)
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.VerifiedActivationTx) bool {
		rst = append(rst, &spacemeshv2.Activation{Versioned: &spacemeshv2.Activation_V1{V1: toAtx(atx)}})
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &spacemeshv2.ActivationList{Activations: rst}, nil
}

func (s *ActivationService) ListHeaders(
	ctx context.Context,
	request *spacemeshv2.ActivationRequest,
) (*spacemeshv2.ActivationHeaderList, error) {
	ops, err := toOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if request.Limit > 10000 {
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 10000")
	} else if request.Limit == 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be set to a value below 10000")
	}
	rst := make([]*spacemeshv2.ActivationHeader, 0, request.Limit)
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.VerifiedActivationTx) bool {
		rst = append(rst, &spacemeshv2.ActivationHeader{Versioned: &spacemeshv2.ActivationHeader_V1{V1: toHeader(atx)}})
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &spacemeshv2.ActivationHeaderList{Headers: rst}, nil
}

func toRequest(filter *spacemeshv2.ActivationStreamRequest) *spacemeshv2.ActivationRequest {
	return &spacemeshv2.ActivationRequest{
		NodeId:     filter.NodeId,
		Id:         filter.Id,
		Coinbase:   filter.Coinbase,
		StartEpoch: filter.StartEpoch,
		EndEpoch:   filter.EndEpoch,
	}
}

func toOperations(filter *spacemeshv2.ActivationRequest) (atxs.Operations, error) {
	ops := atxs.Operations{}
	if filter == nil {
		return ops, nil
	}
	if filter.NodeId != nil {
		ops.Filter = append(ops.Filter, atxs.Op{
			Field: atxs.Smesher,
			Token: atxs.Eq,
			Value: filter.NodeId,
		})
	}
	if filter.Id != nil {
		ops.Filter = append(ops.Filter, atxs.Op{
			Field: atxs.Id,
			Token: atxs.Eq,
			Value: filter.Id,
		})
	}
	if len(filter.Coinbase) > 0 {
		addr, err := types.StringToAddress(filter.Coinbase)
		if err != nil {
			return atxs.Operations{}, err
		}
		ops.Filter = append(ops.Filter, atxs.Op{
			Field: atxs.Coinbase,
			Token: atxs.Eq,
			Value: addr.Bytes(),
		})
	}
	if filter.StartEpoch != 0 {
		ops.Filter = append(ops.Filter, atxs.Op{
			Field: atxs.Epoch,
			Token: atxs.Gte,
			Value: int64(filter.StartEpoch),
		})
	}
	if filter.EndEpoch != 0 {
		ops.Filter = append(ops.Filter, atxs.Op{
			Field: atxs.Epoch,
			Token: atxs.Lte,
			Value: int64(filter.EndEpoch),
		})
	}
	if filter.Offset != 0 {
		ops.Other = append(ops.Other, atxs.Op{
			Field: atxs.Offset,
			Value: int64(filter.Offset),
		})
	}
	if filter.Limit != 0 {
		ops.Other = append(ops.Other, atxs.Op{
			Field: atxs.Limit,
			Value: int64(filter.Limit),
		})
	}
	return ops, nil
}
