package v2alpha1

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const (
	Malfeasance       = "malfeasance_v2alpha1"
	MalfeasanceStream = "malfeasance_stream_v2alpha1"
)

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

func (s *MalfeasanceStreamService) Stream(
	request *spacemeshv2alpha1.MalfeasanceStreamRequest,
	stream spacemeshv2alpha1.MalfeasanceStreamService_StreamServer,
) error {
	ctx := stream.Context()
	var sub *events.BufferedSubscription[events.EventMalfeasance]
	if request.Watch {
		matcher := malfeasanceMatcher{request, ctx}
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

	ops, err := toMalfeasanceOperations(&spacemeshv2alpha1.MalfeasanceRequest{
		SmesherId:    request.SmesherId,
		IncludeProof: request.IncludeProof,
	})
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	go func() {
		defer close(dbChan)
		if err := identities.IterateProofsOps(s.db, ops, func(id types.NodeID, proof *wire.MalfeasanceProof) bool {
			select {
			case dbChan <- toMalfeasance(id, proof, request.IncludeProof):
				return true
			case <-ctx.Done():
				return false
			}
		}); err != nil {
			errChan <- status.Error(codes.Internal, err.Error())
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
			err := stream.Send(toMalfeasance(rst.Smesher, rst.Proof, request.IncludeProof))
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return status.Error(codes.Internal, err.Error())
			}
		default:
			select {
			case rst := <-eventsOut:
				err := stream.Send(toMalfeasance(rst.Smesher, rst.Proof, request.IncludeProof))
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

func (s *MalfeasanceStreamService) String() string {
	return "MalfeasanceStreamService"
}

func NewMalfesanceService(db sql.Executor) *MalfeasanceService {
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

	ops, err := toMalfeasanceOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	rst := make([]*spacemeshv2alpha1.MalfeasanceProof, 0, request.Limit)
	if err := identities.IterateProofsOps(s.db, ops, func(nodeId types.NodeID, proof *wire.MalfeasanceProof) bool {
		rst = append(rst, toMalfeasance(nodeId, proof, request.IncludeProof))
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.MalfeasanceList{Malfeasances: rst}, nil
}

func toMalfeasanceOperations(filter *spacemeshv2alpha1.MalfeasanceRequest) (builder.Operations, error) {
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

	return ops, nil
}

func toMalfeasance(id types.NodeID, proof *wire.MalfeasanceProof,
	includeProof bool,
) *spacemeshv2alpha1.MalfeasanceProof {
	if proof == nil {
		return &spacemeshv2alpha1.MalfeasanceProof{}
	}

	kind := spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_UNSPECIFIED
	switch proof.Proof.Type {
	case wire.MultipleATXs:
		kind = spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_ATX
	case wire.MultipleBallots:
		kind = spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_BALLOT
	case wire.HareEquivocation:
		kind = spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_HARE
	case wire.InvalidPostIndex:
		kind = spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_POST_INDEX
	case wire.InvalidPrevATX:
		kind = spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_INCORRECT_PREV_ATX
	case wire.DoubleMarry:
		kind = spacemeshv2alpha1.MalfeasanceProof_MALFEASANCE_DOUBLE_MARRY
	}

	p := &spacemeshv2alpha1.MalfeasanceProof{
		Smesher:   id.Bytes(),
		Layer:     proof.Layer.Uint32(),
		Kind:      kind,
		DebugInfo: wire.MalfeasanceInfo(id, proof),
	}
	if includeProof {
		data, _ := codec.Encode(proof)
		p.Proof = data
	}

	return p
}

type malfeasanceMatcher struct {
	*spacemeshv2alpha1.MalfeasanceStreamRequest
	ctx context.Context
}

func (m *malfeasanceMatcher) match(t *events.EventMalfeasance) bool {
	if len(m.SmesherId) > 0 {
		idx := slices.IndexFunc(m.SmesherId, func(id []byte) bool { return bytes.Equal(id, t.Smesher.Bytes()) })
		if idx == -1 {
			return false
		}
	}

	return true
}
