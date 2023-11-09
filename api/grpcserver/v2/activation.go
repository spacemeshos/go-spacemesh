package v2

import (
	"context"
	"errors"
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	spacemeshv2 "github.com/spacemeshos/api/release/go/spacemesh/v2"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
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
	// TODO(dshulyak) implement matcher based on filter
	var sub *events.BufferedSubscription[events.ActivationTx]
	if request.Watch {
		var err error
		sub, err = events.Subscribe[events.ActivationTx]()
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
		ierr = stream.Send(&spacemeshv2.Activation{Versioned: &spacemeshv2.Activation_V1{V1: toAtx(atx)}})
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
			if err := stream.Send(&spacemeshv2.Activation{
				Versioned: &spacemeshv2.Activation_V1{V1: toAtx(rst.VerifiedActivationTx)}},
			); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return status.Error(codes.Internal, err.Error())
			}
		}
	}
}

func (s *ActivationStreamService) StreamHeaders(
	request *spacemeshv2.ActivationStreamRequest,
	stream spacemeshv2.ActivationStreamService_StreamHeadersServer,
) error {
	// TODO(dshulyak) the code below is almost the same as code in Stream
	// it can be refactored by implementing generic with toAtx/toHeader

	// TODO(dshulyak) implement matcher based on filter
	var sub *events.BufferedSubscription[events.ActivationTx]
	if request.Watch {
		var err error
		sub, err = events.Subscribe[events.ActivationTx]()
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
		ierr = stream.Send(&spacemeshv2.ActivationHeader{Versioned: &spacemeshv2.ActivationHeader_V1{
			V1: toHeader(atx)}})
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
			if err := stream.Send(&spacemeshv2.ActivationHeader{
				Versioned: &spacemeshv2.ActivationHeader_V1{V1: toHeader(rst.VerifiedActivationTx)}},
			); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return status.Error(codes.Internal, err.Error())
			}
		}
	}
}

func toAtx(atx *types.VerifiedActivationTx) *spacemeshv2.ActivationV1 {
	v1 := &spacemeshv2.ActivationV1{
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
		v1.VrfPostIndex = &spacemeshv2.VRFPostIndex{
			Nonce: uint64(*atx.VRFNonce),
		}
	}
	if atx.InitialPost != nil {
		v1.InitialPost = &spacemeshv2.Post{
			Nonce:   atx.InitialPost.Nonce,
			Indices: atx.InitialPost.Indices,
			Pow:     atx.InitialPost.Pow,
		}
	}
	if nipost := atx.NIPost; nipost != nil {
		if nipost.Post != nil {
			v1.Post = &spacemeshv2.Post{
				Nonce:   nipost.Post.Nonce,
				Indices: nipost.Post.Indices,
				Pow:     nipost.Post.Pow,
			}
		}
		if nipost.PostMetadata != nil {
			v1.PostMeta = &spacemeshv2.PostMeta{
				Challenge: nipost.PostMetadata.Challenge,
				Labels:    nipost.PostMetadata.LabelsPerUnit,
			}
		}
		v1.PoetProof = &spacemeshv2.PoetProof{
			ProofNodes: make([][]byte, len(nipost.Membership.Nodes)),
			Leaf:       nipost.Membership.LeafIndex,
		}
		for i, node := range nipost.Membership.Nodes {
			v1.PoetProof.ProofNodes[i] = node.Bytes()
		}
	}
	return v1
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
