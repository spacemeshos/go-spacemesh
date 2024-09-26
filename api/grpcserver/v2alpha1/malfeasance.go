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

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
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

func NewMalfeasanceService(db sql.Executor, malfeasanceHandler malfeasanceInfo) *MalfeasanceService {
	return &MalfeasanceService{
		db:   db,
		info: malfeasanceHandler,
	}
}

type MalfeasanceService struct {
	db   sql.Executor
	info malfeasanceInfo
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

	proofs := make([]*spacemeshv2alpha1.MalfeasanceProof, 0, request.Limit)
	if err := identities.IterateMaliciousOps(s.db, ops, func(id types.NodeID, proof []byte, received time.Time) bool {
		rst := toProof(ctx, s.info, id, proof)
		if rst == nil {
			return true
		}
		proofs = append(proofs, rst)
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.MalfeasanceList{Proofs: proofs}, nil
}

func NewMalfeasanceStreamService(db sql.Executor, malfeasanceHandler malfeasanceInfo) *MalfeasanceStreamService {
	return &MalfeasanceStreamService{
		db:   db,
		info: malfeasanceHandler,
	}
}

type MalfeasanceStreamService struct {
	db   sql.Executor
	info malfeasanceInfo
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
	var sub *events.BufferedSubscription[events.EventMalfeasance]
	if request.Watch {
		matcher := malfeasanceMatcher{request}
		var err error
		sub, err = events.SubscribeMatched(matcher.match)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		defer sub.Close()
		if err := stream.SendHeader(metadata.MD{}); err != nil {
			return status.Errorf(codes.Unavailable, "can't send header")
		}
	}

	dbChan := make(chan *spacemeshv2alpha1.MalfeasanceProof, 100)
	errChan := make(chan error, 1)

	ops, err := toMalfeasanceOps(&spacemeshv2alpha1.MalfeasanceRequest{
		SmesherId: request.SmesherId,
	})
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// send db data to chan to avoid buffer overflow
	go func() {
		defer close(dbChan)
		if err := identities.IterateMaliciousOps(s.db, ops,
			func(id types.NodeID, proof []byte, received time.Time) bool {
				rst := toProof(stream.Context(), s.info, id, proof)
				if rst == nil {
					return true
				}

				select {
				case dbChan <- rst:
					return true
				case <-stream.Context().Done():
					// exit if the stream is canceled
					return false
				}
			},
		); err != nil {
			errChan <- status.Error(codes.Internal, err.Error())
			return
		}
	}()

	var eventsOut <-chan events.EventMalfeasance
	var eventsFull <-chan struct{}
	if sub != nil {
		eventsOut = sub.Out()
		eventsFull = sub.Full()
	}

	for {
		select {
		case rst := <-eventsOut:
			proof := toProof(stream.Context(), s.info, rst.Smesher, codec.MustEncode(rst.Proof))
			if proof == nil {
				continue
			}
			err = stream.Send(proof)
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return status.Error(codes.Internal, err.Error())
			}
		default:
			select {
			case rst := <-eventsOut:
				proof := toProof(stream.Context(), s.info, rst.Smesher, codec.MustEncode(rst.Proof))
				if err == nil {
					continue
				}
				err = stream.Send(proof)
				switch {
				case errors.Is(err, io.EOF):
					return nil
				case err != nil:
					return status.Error(codes.Internal, err.Error())
				}
			case <-eventsFull:
				return status.Error(codes.Canceled, "buffer overflow")
			case rst, ok := <-dbChan:
				if !ok {
					dbChan = nil
					if sub == nil {
						return nil
					}
					continue
				}
				err = stream.Send(rst)
				switch {
				case errors.Is(err, io.EOF):
					return nil
				case err != nil:
					return status.Error(codes.Internal, err.Error())
				}
			case err := <-errChan:
				return err
			case <-stream.Context().Done():
				return nil
			}
		}
	}
}

func toProof(
	ctx context.Context,
	info malfeasanceInfo,
	id types.NodeID,
	proof []byte,
) *spacemeshv2alpha1.MalfeasanceProof {
	properties, err := info.Info(proof)
	if err != nil {
		ctxzap.Debug(ctx, "failed to get malfeasance info",
			zap.String("smesher", id.String()),
			zap.Error(err),
		)
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
		Domain:     spacemeshv2alpha1.MalfeasanceProof_MalfeasanceDomain(domain),
		Type:       uint32(proofType),
		Properties: properties,
	}
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
