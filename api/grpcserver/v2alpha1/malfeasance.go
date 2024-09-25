package v2alpha1

import (
	"context"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func NewMalfeasanceService(db sql.Executor) *MalfeasanceService {
	return &MalfeasanceService{db: db}
}

type MalfeasanceService struct {
	db sql.Executor
}

func (s *MalfeasanceService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterMalfeasanceServiceServer(server, s)
}

func (s *MalfeasanceService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterMalfeasanceServiceHandlerServer(context.Background(), mux, s)
}

func (s *MalfeasanceService) String() string {
	return "MalfeasanceService"
}

func (s *MalfeasanceService) List(
	ctx context.Context,
	request *spacemeshv2alpha1.MalfeasanceRequest,
) (*spacemeshv2alpha1.MalfeasanceList, error) {
	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	ops, err := toMalfeasanceOps(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	rst := make([]*spacemeshv2alpha1.MalfeasanceProof, 0, request.Limit)
	if err := identities.IterateMaliciousOps(s.db, ops, func(id types.NodeID, proof []byte, received time.Time) bool {
		// TODO (mafa): fetch info about proof from malfeasance service
		proofType := uint32(0)
		properties := make(map[string]string)
		rst = append(rst, &spacemeshv2alpha1.MalfeasanceProof{
			Smesher:    id.Bytes(),
			Domain:     spacemeshv2alpha1.MalfeasanceProof_DOMAIN_UNSPECIFIED,
			Type:       proofType,
			Properties: properties,
		})
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.MalfeasanceList{Malfeasances: rst}, nil
}

func NewMalfeasanceStreamService(db sql.Executor) *MalfeasanceStreamService {
	return &MalfeasanceStreamService{db: db}
}

type MalfeasanceStreamService struct {
	db sql.Executor
}

func (s *MalfeasanceStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterMalfeasanceStreamServiceServer(server, s)
}

func (s *MalfeasanceStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterMalfeasanceStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *MalfeasanceStreamService) String() string {
	return "MalfeasanceStreamService"
}

func (s *MalfeasanceStreamService) Stream(
	request *spacemeshv2alpha1.MalfeasanceStreamRequest,
	stream spacemeshv2alpha1.MalfeasanceStreamService_StreamServer,
) error {
	return nil
}

func toMalfeasanceOps(filter *spacemeshv2alpha1.MalfeasanceRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}

	ops.Filter = append(ops.Filter, builder.Op{
		Field: builder.Proof,
		Token: builder.IsNotNull,
	})

	if len(filter.SmesherId) > 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Smesher,
			Token: builder.In,
			Value: filter.SmesherId,
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "id",
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
