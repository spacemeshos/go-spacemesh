package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const chunksize = 1024

// AdminService exposes endpoints for node administration.
type AdminService struct {
	logger  log.Log
	db      *sql.Database
	dataDir string
}

// NewAdminService creates a new admin grpc service.
func NewAdminService(db *sql.Database, dataDir string, lg log.Log) *AdminService {
	return &AdminService{
		logger:  lg,
		db:      db,
		dataDir: dataDir,
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
	snapshot := types.LayerID(req.SnapshotLayer)
	err := checkpoint.Generate(stream.Context(), afero.NewOsFs(), a.db, a.dataDir, snapshot)
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("failed to create checkpoint: %s", err.Error()))
	}
	fname := checkpoint.SelfCheckpointFilename(a.dataDir, snapshot)
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

func (a AdminService) Recover(ctx context.Context, req *pb.RecoverRequest) (*empty.Empty, error) {
	if err := checkpoint.ReadCheckpointAndDie(ctx, a.logger, a.dataDir, req.Uri, types.LayerID(req.RestoreLayer)); err != nil {
		a.logger.WithContext(ctx).With().Error("failed to read checkpoint file", log.Err(err))
		return nil, status.Errorf(codes.Internal,
			fmt.Sprintf("read checkpoint %s and restore %d: %s", req.Uri, req.RestoreLayer, err.Error()))
	}
	return &empty.Empty{}, nil
}
