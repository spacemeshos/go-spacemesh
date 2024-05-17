package v2alpha1

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

const (
	Layer       = "layer_v2alpha1"
	LayerStream = "layer_stream_v2alpha1"
)

func NewLayerStreamService(db sql.Executor) *LayerStreamService {
	return &LayerStreamService{db: db}
}

type LayerStreamService struct {
	db sql.Executor
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
	ctx := stream.Context()
	var sub *events.BufferedSubscription[events.LayerUpdate]
	if request.Watch {
		matcher := layersMatcher{request, ctx}
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

	dbChan := make(chan *spacemeshv2alpha1.Layer, 100)
	errChan := make(chan error, 1)

	ops, err := toLayerOperations(toLayerRequest(request))
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	// send db data to chan to avoid buffer overflow
	go func() {
		defer close(dbChan)
		if err := layers.IterateLayersWithBlockOps(s.db, ops, func(layer *layers.Layer) bool {
			select {
			case dbChan <- &spacemeshv2alpha1.Layer{Versioned: &spacemeshv2alpha1.Layer_V1{
				V1: toLayer(layer),
			}}:
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

	var eventsOut <-chan events.LayerUpdate
	var eventsFull <-chan struct{}
	if sub != nil {
		eventsOut = sub.Out()
		eventsFull = sub.Full()
	}

	for {
		select {
		case rst := <-eventsOut:
			var derr error
			if layer, err := layers.Get(s.db, rst.LayerID); err == nil {
				l := toLayer(layer)
				l.Status = convertEventStatus(rst.Status)

				derr = stream.Send(&spacemeshv2alpha1.Layer{Versioned: &spacemeshv2alpha1.Layer_V1{
					V1: l,
				}})
			} else {
				return status.Error(codes.Internal, derr.Error())
			}

			switch {
			case errors.Is(derr, io.EOF):
				return nil
			case derr != nil:
				return status.Error(codes.Internal, derr.Error())
			}
		default:
			select {
			case rst := <-eventsOut:
				var derr error
				if layer, err := layers.Get(s.db, rst.LayerID); err == nil {
					l := toLayer(layer)
					l.Status = convertEventStatus(rst.Status)

					derr = stream.Send(&spacemeshv2alpha1.Layer{Versioned: &spacemeshv2alpha1.Layer_V1{
						V1: l,
					}})
				} else {
					return status.Error(codes.Internal, derr.Error())
				}

				switch {
				case errors.Is(derr, io.EOF):
					return nil
				case derr != nil:
					return status.Error(codes.Internal, derr.Error())
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
				err := stream.Send(rst)
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

func toLayerRequest(filter *spacemeshv2alpha1.LayerStreamRequest) *spacemeshv2alpha1.LayerRequest {
	req := &spacemeshv2alpha1.LayerRequest{
		StartLayer: filter.StartLayer,
		EndLayer:   filter.EndLayer,
	}
	return req
}

func (s *LayerStreamService) String() string {
	return "LayerStreamService"
}

func NewLayerService(db sql.Executor) *LayerService {
	return &LayerService{
		db: db,
	}
}

type LayerService struct {
	db sql.Executor
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
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	rst := make([]*spacemeshv2alpha1.Layer, 0, request.Limit)
	var derr error
	if err := layers.IterateLayersWithBlockOps(s.db, ops, func(layer *layers.Layer) bool {
		rst = append(rst, &spacemeshv2alpha1.Layer{Versioned: &spacemeshv2alpha1.Layer_V1{
			V1: toLayer(layer),
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
			Prefix: "l.",
			Field:  builder.Id,
			Token:  builder.Gte,
			Value:  int64(filter.StartLayer),
		})
	}

	if filter.EndLayer != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Prefix: "l.",
			Field:  builder.Id,
			Token:  builder.Lte,
			Value:  int64(filter.EndLayer),
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "l.id asc",
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

func toLayer(layer *layers.Layer) *spacemeshv2alpha1.LayerV1 {
	v1 := &spacemeshv2alpha1.LayerV1{
		Number: layer.Id.Uint32(),
	}

	v1.Status = spacemeshv2alpha1.LayerV1_LAYER_STATUS_UNSPECIFIED
	if !layer.AppliedBlock.IsEmpty() {
		v1.Status = spacemeshv2alpha1.LayerV1_LAYER_STATUS_APPLIED
	}
	if layer.Processed {
		v1.Status = spacemeshv2alpha1.LayerV1_LAYER_STATUS_VERIFIED
	}

	if !bytes.Equal(layer.AggregatedHash.Bytes(), types.Hash32{}.Bytes()) {
		v1.ConsensusHash = layer.AggregatedHash.ShortString()
		v1.CumulativeStateHash = layer.AggregatedHash.Bytes()
	}

	if !bytes.Equal(layer.StateHash.Bytes(), types.Hash32{}.Bytes()) {
		v1.StateHash = layer.StateHash.Bytes()
	}

	if layer.Block != nil {
		v1.Block = &spacemeshv2alpha1.Block{
			Versioned: &spacemeshv2alpha1.Block_V1{
				V1: &spacemeshv2alpha1.BlockV1{
					Id: types.Hash20(layer.Block.ID()).Bytes(),
				},
			},
		}
	}

	return v1
}

type layersMatcher struct {
	*spacemeshv2alpha1.LayerStreamRequest
	ctx context.Context
}

func (m *layersMatcher) match(l *events.LayerUpdate) bool {
	if m.StartLayer != 0 {
		if l.LayerID.Uint32() < m.StartLayer {
			return false
		}
	}

	if m.EndLayer != 0 {
		if l.LayerID.Uint32() > m.EndLayer {
			return false
		}
	}

	return true
}

func convertEventStatus(eventStatus int) (status spacemeshv2alpha1.LayerV1_LayerStatus) {
	status = spacemeshv2alpha1.LayerV1_LAYER_STATUS_UNSPECIFIED
	if eventStatus == events.LayerStatusTypeApproved {
		status = spacemeshv2alpha1.LayerV1_LAYER_STATUS_APPLIED
	}
	if eventStatus == events.LayerStatusTypeConfirmed || eventStatus == events.LayerStatusTypeApplied {
		status = spacemeshv2alpha1.LayerV1_LAYER_STATUS_VERIFIED
	}
	return
}
