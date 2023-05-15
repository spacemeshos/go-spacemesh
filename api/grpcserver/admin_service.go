package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const chunksize = 1024

type CheckpointRunnerFunc func() CheckpointRunner

// AdminService exposes endpoints for node administration.
type AdminService struct {
	checkpoint CheckpointRunnerFunc
}

// NewAdminService creates a new admin grpc service.
func NewAdminService(cp CheckpointRunnerFunc) *AdminService {
	return &AdminService{
		checkpoint: cp,
	}
}

// RegisterService registers this service with a grpc server instance.
func (a AdminService) RegisterService(server *Server) {
	pb.RegisterAdminServiceServer(server.GrpcServer, a)
}

func (a AdminService) CheckpointStream(req *pb.CheckpointStreamRequest, stream pb.AdminService_CheckpointStreamServer) error {
	// checkpoint data can be more than 4MB, it can cause stress
	// - on the client side (default limit on the receiving end)
	// - locally as the node already loads db query result in memory
	fname, err := a.checkpoint().Generate(stream.Context(), types.LayerID(req.SnapshotLayer), types.LayerID(req.RestoreLayer))
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("failed to create checkpoint: %s", err.Error()))
	}
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}
	f, err := os.Open(fname)
	defer func() {
		_ = f.Close()
	}()
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("failed to open file %s: %s", fname, err.Error()))
	}
	var (
		buf   = make([]byte, chunksize)
		chunk int
	)
	for {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			chunk, err = f.Read(buf)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return status.Errorf(codes.Internal, fmt.Sprintf("failed to read from file %s: %s", fname, err.Error()))
			}
			if err = stream.Send(&pb.CheckpointStreamResponse{Data: buf[:chunk]}); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		}
	}
}

func (a AdminService) Recover(_ context.Context, _ *pb.RecoverRequest) (*empty.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}
