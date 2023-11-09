package grpcserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

type activationService struct {
	goldenAtx   types.ATXID
	db          *sql.Database
	atxProvider atxProvider
}

func NewActivationService(db *sql.Database, atxProvider atxProvider, goldenAtx types.ATXID) *activationService {
	return &activationService{
		db:          db,
		goldenAtx:   goldenAtx,
		atxProvider: atxProvider,
	}
}

// RegisterService implements ServiceAPI.
func (s *activationService) RegisterService(server *grpc.Server) {
	pb.RegisterActivationServiceServer(server, s)
}

func (s *activationService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterActivationServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *activationService) String() string {
	return "ActivationService"
}

// Get implements v1.ActivationServiceServer.
func (s *activationService) Get(ctx context.Context, request *pb.GetRequest) (*pb.GetResponse, error) {
	if l := len(request.Id); l != types.ATXIDSize {
		return nil, status.Error(
			codes.InvalidArgument,
			fmt.Sprintf("invalid ATX ID length (%d), expected (%d)", l, types.ATXIDSize),
		)
	}

	atxId := types.ATXID(types.BytesToHash(request.Id))
	atx, err := s.atxProvider.GetFullAtx(atxId)
	if err != nil || atx == nil {
		ctxzap.Debug(ctx, "failed to get ATX",
			zap.Stringer("id", atxId),
			zap.Error(err),
		)
		return nil, status.Error(codes.NotFound, "id was not found")
	}
	proof, err := s.atxProvider.GetMalfeasanceProof(atx.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		ctxzap.Error(ctx, "failed to get malfeasance proof",
			zap.Stringer("smesher", atx.SmesherID),
			zap.Stringer("smesher", atx.SmesherID),
			zap.Stringer("id", atxId),
			zap.Error(err),
		)
		return nil, status.Error(codes.NotFound, "id was not found")
	}
	resp := &pb.GetResponse{
		Atx: convertActivation(atx),
	}
	if proof != nil {
		resp.MalfeasanceProof = events.ToMalfeasancePB(atx.SmesherID, proof, false)
	}
	return resp, nil
}

func (s *activationService) Highest(ctx context.Context, req *emptypb.Empty) (*pb.HighestResponse, error) {
	highest, err := s.atxProvider.MaxHeightAtx()
	if err != nil {
		return &pb.HighestResponse{
			Atx: &pb.Activation{
				Id: &pb.ActivationId{Id: s.goldenAtx.Bytes()},
			},
		}, nil
	}
	atx, err := s.atxProvider.GetFullAtx(highest)
	if err != nil || atx == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("atx id %v not found: %v", highest, err.Error()))
	}
	return &pb.HighestResponse{
		Atx: convertActivation(atx),
	}, nil
}

func (s *activationService) Stream(filter *pb.ActivationStreamRequest, stream pb.ActivationService_StreamServer) error {
	if filter.Watch {
		return status.Error(codes.InvalidArgument, "watch is not supported")
	}
	var ierr error
	if err := atxs.IterateAtxsOps(s.db, toOperations(filter), func(atx *types.VerifiedActivationTx) bool {
		v1 := &pb.ActivationV1{
			Id:             atx.ID().Bytes(),
			NodeId:         atx.SmesherID.Bytes(),
			Signature:      atx.Signature.Bytes(),
			PublishEpoch:   atx.PublishEpoch.Uint32(),
			Sequence:       atx.Sequence,
			PrevAtx:        atx.PrevATXID[:],
			PositioningAtx: atx.PositioningATX[:],
			Coinbase:       atx.Coinbase.String(),
			Units:          atx.NumUnits,
			BaseTick:       uint32(atx.BaseTickHeight()),
			Ticks:          uint32(atx.TickCount()),
		}
		ierr = stream.Send(&pb.ActivationStreamResponse{V1: v1})
		return ierr == nil
	}); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
}

func toOperations(filter *pb.ActivationStreamRequest) atxs.Operations {
	return atxs.Operations{}
}
