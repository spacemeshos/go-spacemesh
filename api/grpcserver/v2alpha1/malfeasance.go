package v2alpha1

import (
	"context"
	"strconv"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const (
	Malfeasance       = "malfeasance_v2alpha1"
	MalfeasanceStream = "malfeasance_stream_v2alpha1"
)

type malfeasanceInfo interface {
	Info(data []byte) (map[string]string, error)
}

func NewMalfeasanceService(
	db sql.Executor,
	logger *zap.Logger,
	malfeasanceHandler malfeasanceInfo,
) *MalfeasanceService {
	return &MalfeasanceService{
		db:     db,
		logger: logger,
		info:   malfeasanceHandler,
	}
}

type MalfeasanceService struct {
	db     sql.Executor
	logger *zap.Logger
	info   malfeasanceInfo
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
		result := s.toProof(id, proof)
		if result == nil {
			return true
		}
		rst = append(rst, result)
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.MalfeasanceList{Proofs: rst}, nil
}

func (s *MalfeasanceService) toProof(id types.NodeID, proof []byte) *spacemeshv2alpha1.MalfeasanceProof {
	properties, err := s.info.Info(proof)
	if err != nil {
		s.logger.Debug("failed to get malfeasance info",
			zap.String("smesher", id.String()),
			zap.Error(err),
		)
		return nil
	}
	domain, err := strconv.ParseUint(properties["domain"], 10, 64)
	if err != nil {
		s.logger.Debug("failed to parse proof domain",
			zap.String("smesher", id.String()),
			zap.String("domain", properties["domain"]),
			zap.Error(err),
		)
		return nil
	}
	delete(properties, "domain")
	proofType, err := strconv.ParseUint(properties["type"], 10, 32)
	if err != nil {
		s.logger.Debug("failed to parse proof type",
			zap.String("smesher", id.String()),
			zap.String("type", properties["type"]),
			zap.Error(err),
		)
		return nil
	}
	delete(properties, "type")
	return &spacemeshv2alpha1.MalfeasanceProof{
		Smesher:    id.Bytes(),
		Domain:     spacemeshv2alpha1.MalfeasanceProof_MalfeasanceDomain(domain),
		Type:       uint32(proofType),
		Properties: properties,
	}
}

func NewMalfeasanceStreamService(
	db sql.Executor,
	logger *zap.Logger,
	malfeasanceHandler malfeasanceInfo,
) *MalfeasanceStreamService {
	return &MalfeasanceStreamService{
		db:     db,
		logger: logger,
		info:   malfeasanceHandler,
	}
}

type MalfeasanceStreamService struct {
	db     sql.Executor
	logger *zap.Logger
	info   malfeasanceInfo
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
	ops.Filter = append(ops.Filter, builder.Op{
		Field: builder.Proof,
		Token: builder.IsNotNull,
	})
	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: builder.Smesher,
	})

	if filter == nil {
		return ops, nil
	}

	if len(filter.SmesherId) > 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Smesher,
			Token: builder.In,
			Value: filter.SmesherId,
		})
	}

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
