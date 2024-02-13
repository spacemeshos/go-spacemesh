package v2alpha1

import (
	"context"
	"errors"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"go.uber.org/zap"
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const (
	Activation       = "activation_v2alpha1"
	ActivationStream = "activation_stream_v2alpha1"
)

func NewActivationStreamService(db *sql.Database) *ActivationStreamService {
	return &ActivationStreamService{db: db}
}

type ActivationStreamService struct {
	db *sql.Database
}

var _ grpcserver.ServiceAPI = (*ActivationStreamService)(nil)

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
	var sub *events.BufferedSubscription[events.ActivationTx]
	if request.Watch {
		matcher := resultsMatcher{request, stream.Context()}
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
	ops, err := toOperations(toRequest(request))
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	var ierr error
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.VerifiedActivationTx) bool {
		ierr = stream.Send(&spacemeshv2alpha1.Activation{Versioned: &spacemeshv2alpha1.Activation_V1{V1: toAtx(atx)}})
		return ierr == nil
	}); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if sub == nil {
		return nil
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-sub.Full():
			return status.Error(codes.Canceled, "buffer overflow")
		case rst := <-sub.Out():
			if err := stream.Send(&spacemeshv2alpha1.Activation{
				Versioned: &spacemeshv2alpha1.Activation_V1{V1: toAtx(rst.VerifiedActivationTx)}},
			); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return status.Error(codes.Internal, err.Error())
			}
		}
	}
}

func toAtx(atx *types.VerifiedActivationTx) *spacemeshv2alpha1.ActivationV1 {
	v1 := &spacemeshv2alpha1.ActivationV1{
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
	if atx.CommitmentATX != nil {
		v1.CommittmentAtx = atx.CommitmentATX.Bytes()
	}
	if atx.VRFNonce != nil {
		v1.VrfPostIndex = &spacemeshv2alpha1.VRFPostIndex{
			Nonce: uint64(*atx.VRFNonce),
		}
	}
	if atx.InitialPost != nil {
		v1.InitialPost = &spacemeshv2alpha1.Post{
			Nonce:   atx.InitialPost.Nonce,
			Indices: atx.InitialPost.Indices,
			Pow:     atx.InitialPost.Pow,
		}
	}
	if nipost := atx.NIPost; nipost != nil {
		if nipost.Post != nil {
			v1.Post = &spacemeshv2alpha1.Post{
				Nonce:   nipost.Post.Nonce,
				Indices: nipost.Post.Indices,
				Pow:     nipost.Post.Pow,
			}
		}
		if nipost.PostMetadata != nil {
			v1.PostMeta = &spacemeshv2alpha1.PostMeta{
				Challenge: nipost.PostMetadata.Challenge,
				Labels:    nipost.PostMetadata.LabelsPerUnit,
			}
		}
		v1.PoetProof = &spacemeshv2alpha1.PoetProof{
			ProofNodes: make([][]byte, len(nipost.Membership.Nodes)),
			Leaf:       nipost.Membership.LeafIndex,
		}
		for i, node := range nipost.Membership.Nodes {
			v1.PoetProof.ProofNodes[i] = node.Bytes()
		}
	}
	return v1
}

func NewActivationService(db *sql.Database) *ActivationService {
	return &ActivationService{db: db}
}

type ActivationService struct {
	db *sql.Database
}

var _ grpcserver.ServiceAPI = (*ActivationService)(nil)

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
	ops, err := toOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// every full atx is ~1KB. 100 atxs is ~100KB.
	if request.Limit > 100 {
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	} else if request.Limit == 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}
	rst := make([]*spacemeshv2alpha1.Activation, 0, request.Limit)
	if err := atxs.IterateAtxsOps(s.db, ops, func(atx *types.VerifiedActivationTx) bool {
		rst = append(rst, &spacemeshv2alpha1.Activation{Versioned: &spacemeshv2alpha1.Activation_V1{V1: toAtx(atx)}})
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
	ops := builder.Operations{Filter: []builder.Op{
		{
			Field: builder.Epoch,
			Token: builder.Eq,
			Value: int64(request.Epoch),
		},
	}}

	count, err := atxs.CountAtxsByEpoch(s.db, ops)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.ActivationsCountResponse{Count: count}, nil

}

func toRequest(filter *spacemeshv2alpha1.ActivationStreamRequest) *spacemeshv2alpha1.ActivationRequest {
	return &spacemeshv2alpha1.ActivationRequest{
		NodeId:     filter.NodeId,
		Id:         filter.Id,
		Coinbase:   filter.Coinbase,
		StartEpoch: filter.StartEpoch,
		EndEpoch:   filter.EndEpoch,
	}
}

func toOperations(filter *spacemeshv2alpha1.ActivationRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}
	if filter.NodeId != nil {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Smesher,
			Token: builder.Eq,
			Value: filter.NodeId,
		})
	}
	if filter.Id != nil {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Id,
			Token: builder.Eq,
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

	ops.Other = append(ops.Other, builder.Op{
		Field: builder.OrderBy,
		Value: "epoch asc, id",
	})

	if filter.Limit != 0 {
		ops.Other = append(ops.Other, builder.Op{
			Field: builder.Limit,
			Value: int64(filter.Limit),
		})
	}
	if filter.Offset != 0 {
		ops.Other = append(ops.Other, builder.Op{
			Field: builder.Offset,
			Value: int64(filter.Offset),
		})
	}

	return ops, nil
}

type resultsMatcher struct {
	*spacemeshv2alpha1.ActivationStreamRequest
	ctx context.Context
}

func (m *resultsMatcher) match(t *events.ActivationTx) bool {
	if len(m.NodeId) > 0 {
		var nodeId types.NodeID
		copy(nodeId[:], m.NodeId)

		if t.SmesherID != nodeId {
			return false
		}
	}

	if len(m.Id) > 0 {
		var atxId types.ATXID
		copy(atxId[:], m.Id)

		if t.ID() != atxId {
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
