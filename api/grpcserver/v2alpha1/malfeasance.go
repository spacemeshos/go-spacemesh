package v2alpha1

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
)

const (
	Malfeasance       = "malfeasance_v2alpha1"
	MalfeasanceStream = "malfeasance_stream_v2alpha1"
)

func NewMalfeasanceService(db sql.StateDatabase, malHandler, legacyHandler malfeasanceInfo) *MalfeasanceService {
	return &MalfeasanceService{
		db:         db,
		info:       malHandler,
		infoLegacy: legacyHandler,
	}
}

type MalfeasanceService struct {
	db         sql.StateDatabase
	info       malfeasanceInfo
	infoLegacy malfeasanceInfo
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

	result := &spacemeshv2alpha1.MalfeasanceList{}
	err := s.db.WithTx(ctx, func(tx sql.Transaction) error {
		legacyCount, err := identities.CountMalicious(tx)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}

		switch {
		case request.Offset+request.Limit < legacyCount: // only legacy proofs
			proofs, err := fetchLegacyFromDB(ctx, tx, s.infoLegacy, request)
			if err != nil {
				return err
			}
			result.Proofs = proofs
			return nil
		case request.Offset >= legacyCount: // only new proofs
			request.Offset -= legacyCount
			proofs, err := fetchFromDB(ctx, tx, s.info, request)
			if err != nil {
				return err
			}
			result.Proofs = proofs
			return nil
		default: // both legacy and new proofs
			legacyProofs, err := fetchLegacyFromDB(ctx, tx, s.infoLegacy, request)
			if err != nil {
				return err
			}
			request.Limit -= uint64(len(legacyProofs))
			proofs, err := fetchFromDB(ctx, tx, s.info, request)
			if err != nil {
				return err
			}
			result.Proofs = append(legacyProofs, proofs...)
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func NewMalfeasanceStreamService(
	db sql.Executor,
	malfeasanceHandler,
	legacyHandler malfeasanceInfo,
) *MalfeasanceStreamService {
	return &MalfeasanceStreamService{
		db:         db,
		info:       malfeasanceHandler,
		infoLegacy: legacyHandler,
	}
}

type MalfeasanceStreamService struct {
	db         sql.Executor
	info       malfeasanceInfo
	infoLegacy malfeasanceInfo
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
	legacyProofs, err := fetchLegacyFromDB(
		stream.Context(),
		s.db,
		s.infoLegacy,
		&spacemeshv2alpha1.MalfeasanceRequest{SmesherId: request.SmesherId},
	)
	if err != nil {
		return err
	}
	for _, rst := range legacyProofs {
		err := stream.Send(rst)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return status.Error(codes.Internal, err.Error())
		}
	}

	proofs, err := fetchFromDB(
		stream.Context(),
		s.db,
		s.info,
		&spacemeshv2alpha1.MalfeasanceRequest{SmesherId: request.SmesherId},
	)
	if err != nil {
		return err
	}
	for _, rst := range proofs {
		err := stream.Send(rst)
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return status.Error(codes.Internal, err.Error())
		}
	}

	if !request.Watch {
		return nil
	}

	matcher := malfeasanceMatcher{request}
	sub, err := events.SubscribeMatched(matcher.match)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	defer sub.Close()
	eventsOut := sub.Out()
	eventsFull := sub.Full()

	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}

	for {
		select {
		// process pending events first
		case rst := <-eventsOut:
			proof := fetchMetaData(stream.Context(), s.infoLegacy, rst.Smesher)
			if proof == nil {
				// try again with the new handler
				proof = fetchMetaData(stream.Context(), s.info, rst.Smesher)
				if proof == nil {
					ctxzap.Debug(stream.Context(), "failed to get malfeasance info",
						zap.String("smesher", rst.Smesher.String()),
						zap.Error(err),
					)
					continue
				}
			}
			err := stream.Send(proof)
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return status.Error(codes.Internal, err.Error())
			}
		default:
			select {
			case rst := <-eventsOut:
				proof := fetchMetaData(stream.Context(), s.infoLegacy, rst.Smesher)
				if proof == nil {
					// try again with the new handler
					proof = fetchMetaData(stream.Context(), s.info, rst.Smesher)
					if proof == nil {
						ctxzap.Debug(stream.Context(), "failed to get malfeasance info",
							zap.String("smesher", rst.Smesher.String()),
							zap.Error(err),
						)
						continue
					}
				}
				err := stream.Send(proof)
				switch {
				case errors.Is(err, io.EOF):
					return nil
				case err != nil:
					return status.Error(codes.Internal, err.Error())
				}
			case <-eventsFull:
				return status.Error(codes.Canceled, "buffer overflow")
			case <-stream.Context().Done():
				return nil
			}
		}
	}
}

func fetchMetaData(
	ctx context.Context,
	info malfeasanceInfo,
	id types.NodeID,
) *spacemeshv2alpha1.MalfeasanceProof {
	properties, err := info.Info(ctx, id)
	if err != nil {
		return nil
	}
	domain, err := strconv.ParseUint(properties["domain"], 10, 64)
	if err != nil {
		ctxzap.Debug(ctx, "failed to parse proof domain",
			zap.String("smesher", id.String()),
			zap.String("domain", properties["domain"]),
			zap.Error(err),
		)
		return nil
	}
	delete(properties, "domain")
	proofType, err := strconv.ParseUint(properties["type"], 10, 32)
	if err != nil {
		ctxzap.Debug(ctx, "failed to parse proof type",
			zap.String("smesher", id.String()),
			zap.String("type", properties["type"]),
			zap.Error(err),
		)
		return nil
	}
	delete(properties, "type")
	return &spacemeshv2alpha1.MalfeasanceProof{
		Smesher:    id.Bytes(),
		Domain:     spacemeshv2alpha1.MalfeasanceProof_MalfeasanceDomain(domain), // TODO(mafa): add new domains
		Type:       uint32(proofType),
		Properties: properties,
	}
}

func fetchFromDB(
	ctx context.Context,
	db sql.Executor,
	info malfeasanceInfo,
	request *spacemeshv2alpha1.MalfeasanceRequest,
) ([]*spacemeshv2alpha1.MalfeasanceProof, error) {
	ops, err := toMalfeasanceOps(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	ids := make([]types.NodeID, 0, request.Limit)
	if err := malfeasance.IterateOps(db, ops, func(id types.NodeID, _ []byte, _ int, _ time.Time) bool {
		ids = append(ids, id)
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	proofs := make([]*spacemeshv2alpha1.MalfeasanceProof, 0, len(ids))
	for _, id := range ids {
		rst := fetchMetaData(ctx, info, id)
		if rst == nil {
			continue
		}
		proofs = append(proofs, rst)
	}
	return proofs, nil
}

func fetchLegacyFromDB(
	ctx context.Context,
	db sql.Executor,
	info malfeasanceInfo,
	request *spacemeshv2alpha1.MalfeasanceRequest,
) ([]*spacemeshv2alpha1.MalfeasanceProof, error) {
	ops, err := toMalfeasanceOps(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	ids := make([]types.NodeID, 0, request.Limit)
	if err := identities.IterateOps(db, ops, func(id types.NodeID, _ []byte, _ time.Time) bool {
		ids = append(ids, id)
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	proofs := make([]*spacemeshv2alpha1.MalfeasanceProof, 0, len(ids))
	for _, id := range ids {
		rst := fetchMetaData(ctx, info, id)
		if rst == nil {
			continue
		}
		proofs = append(proofs, rst)
	}
	return proofs, nil
}

func toMalfeasanceOps(filter *spacemeshv2alpha1.MalfeasanceRequest) (builder.Operations, error) {
	ops := builder.Operations{}
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

type malfeasanceMatcher struct {
	*spacemeshv2alpha1.MalfeasanceStreamRequest
}

func (m *malfeasanceMatcher) match(event *events.EventMalfeasance) bool {
	if len(m.SmesherId) > 0 {
		idx := slices.IndexFunc(m.SmesherId, func(id []byte) bool { return bytes.Equal(id, event.Smesher.Bytes()) })
		if idx == -1 {
			return false
		}
	}
	return true
}
