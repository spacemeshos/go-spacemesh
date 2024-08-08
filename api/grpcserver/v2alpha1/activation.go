package v2alpha1

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"

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
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

const (
	Activation       = "activation_v2alpha1"
	ActivationStream = "activation_stream_v2alpha1"
)

func NewActivationStreamService(db sql.Executor) *ActivationStreamService {
	return &ActivationStreamService{db: db}
}

type ActivationStreamService struct {
	db sql.Executor
}

func (s *ActivationStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterActivationStreamServiceServer(server, s)
}

func (s *ActivationStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterActivationStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *ActivationStreamService) String() string {
	return "ActivationStreamService"
}

func (s *ActivationStreamService) Stream(
	request *spacemeshv2alpha1.ActivationStreamRequest,
	stream spacemeshv2alpha1.ActivationStreamService_StreamServer,
) error {
	ctx := stream.Context()
	var sub *events.BufferedSubscription[events.ActivationTx]
	if request.Watch {
		matcher := atxsMatcher{request, ctx}
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

	dbChan := make(chan *types.ActivationTx, 100)
	errChan := make(chan error, 1)

	ops, err := toAtxOperations(toAtxRequest(request))
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// send db data to chan to avoid buffer overflow
	go func() {
		defer close(dbChan)
		if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.ActivationTx) bool {
			select {
			case dbChan <- atx:
				return true
			case <-ctx.Done():
				// exit if the stream context is canceled
				return false
			}
		}); err != nil {
			errChan <- status.Error(codes.Internal, err.Error())
			return
		}
	}()

	var eventsOut <-chan events.ActivationTx
	var eventsFull <-chan struct{}
	if sub != nil {
		eventsOut = sub.Out()
		eventsFull = sub.Full()
	}

	for {
		select {
		case rst := <-eventsOut:
			err := stream.Send(toAtx(rst.ActivationTx))
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return status.Error(codes.Internal, err.Error())
			}
		default:
			select {
			case rst := <-eventsOut:
				err := stream.Send(toAtx(rst.ActivationTx))
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
				err := stream.Send(toAtx(rst))
				switch {
				case errors.Is(err, io.EOF):
					return nil
				case err != nil:
					return status.Error(codes.Internal, err.Error())
				}
			case err := <-errChan:
				return err
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func toAtx(atx *types.ActivationTx) *spacemeshv2alpha1.Activation {
	return &spacemeshv2alpha1.Activation{
		Id:           atx.ID().Bytes(),
		SmesherId:    atx.SmesherID.Bytes(),
		PublishEpoch: atx.PublishEpoch.Uint32(),
		Coinbase:     atx.Coinbase.String(),
		Weight:       atx.Weight,
		Height:       atx.TickHeight(),
		NumUnits:     atx.NumUnits,
	}
}

func NewActivationService(db sql.Executor) *ActivationService {
	return &ActivationService{db: db}
}

type ActivationService struct {
	db sql.Executor
}

func (s *ActivationService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterActivationServiceServer(server, s)
}

func (s *ActivationService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterActivationServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *ActivationService) String() string {
	return "ActivationService"
}

func (s *ActivationService) List(
	ctx context.Context,
	request *spacemeshv2alpha1.ActivationRequest,
) (*spacemeshv2alpha1.ActivationList, error) {
	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	ops, err := toAtxOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// every full atx is ~1KB. 100 atxs is ~100KB.
	rst := make([]*spacemeshv2alpha1.Activation, 0, request.Limit)
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.ActivationTx) bool {
		rst = append(rst, toAtx(atx))
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.ActivationList{Activations: rst}, nil
}

func (s *ActivationService) ActivationsCount(
	ctx context.Context,
	request *spacemeshv2alpha1.ActivationsCountRequest,
) (*spacemeshv2alpha1.ActivationsCountResponse, error) {
	ops := builder.Operations{}
	if request.Epoch != nil {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Epoch,
			Token: builder.Eq,
			Value: int64(*request.Epoch),
		})
	}

	count, err := atxs.CountAtxsByOps(s.db, ops)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.ActivationsCountResponse{Count: count}, nil
}

func toAtxRequest(filter *spacemeshv2alpha1.ActivationStreamRequest) *spacemeshv2alpha1.ActivationRequest {
	return &spacemeshv2alpha1.ActivationRequest{
		SmesherId:  filter.SmesherId,
		Id:         filter.Id,
		Coinbase:   filter.Coinbase,
		StartEpoch: filter.StartEpoch,
		EndEpoch:   filter.EndEpoch,
	}
}

func toAtxOperations(filter *spacemeshv2alpha1.ActivationRequest) (builder.Operations, error) {
	ops := builder.Operations{}
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
	if len(filter.Id) > 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Id,
			Token: builder.In,
			Value: filter.Id,
		})
	}
	if len(filter.Coinbase) > 0 {
		addr, err := types.StringToAddress(filter.Coinbase)
		if err != nil {
			return builder.Operations{}, err
		}
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Coinbase,
			Token: builder.Eq,
			Value: addr.Bytes(),
		})
	}
	if filter.StartEpoch != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Epoch,
			Token: builder.Gte,
			Value: int64(filter.StartEpoch),
		})
	}
	if filter.EndEpoch != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Epoch,
			Token: builder.Lte,
			Value: int64(filter.EndEpoch),
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "epoch asc, id",
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

type atxsMatcher struct {
	*spacemeshv2alpha1.ActivationStreamRequest
	ctx context.Context
}

func (m *atxsMatcher) match(t *events.ActivationTx) bool {
	if len(m.SmesherId) > 0 {
		idx := slices.IndexFunc(m.SmesherId, func(id []byte) bool { return bytes.Equal(id, t.SmesherID.Bytes()) })
		if idx == -1 {
			return false
		}
	}

	if len(m.Id) > 0 {
		idx := slices.IndexFunc(m.Id, func(id []byte) bool { return bytes.Equal(id, t.ID().Bytes()) })
		if idx == -1 {
			return false
		}
	}

	if len(m.Coinbase) > 0 {
		addr, err := types.StringToAddress(m.Coinbase)
		if err != nil {
			ctxzap.Error(m.ctx, "unable to convert atx coinbase", zap.Error(err))
			return false
		}
		if t.Coinbase != addr {
			return false
		}
	}

	if m.StartEpoch != 0 {
		if t.PublishEpoch.Uint32() < m.StartEpoch {
			return false
		}
	}

	if m.EndEpoch != 0 {
		if t.PublishEpoch.Uint32() > m.EndEpoch {
			return false
		}
	}

	return true
}
