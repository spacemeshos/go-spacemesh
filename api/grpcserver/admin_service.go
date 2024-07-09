package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	chunksize      = 1024
	defaultNumAtxs = 4
)

// AdminService exposes endpoints for node administration.
type AdminService struct {
	db      *sql.Database
	dataDir string
	recover func()
	p       peers
}

// NewAdminService creates a new admin grpc service.
func NewAdminService(db *sql.Database, dataDir string, p peers) *AdminService {
	return &AdminService{
		db:      db,
		dataDir: dataDir,
		recover: func() {
			go func() {
				// Allow time for the response to be sent.
				time.Sleep(time.Second)
				os.Exit(0)
			}()
		},
		p: p,
	}
}

// RegisterService registers this service with a grpc server instance.
func (a AdminService) RegisterService(server *grpc.Server) {
	pb.RegisterAdminServiceServer(server, a)
}

func (s AdminService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterAdminServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (a AdminService) String() string {
	return "AdminService"
}

func (a AdminService) CheckpointStream(
	req *pb.CheckpointStreamRequest,
	stream pb.AdminService_CheckpointStreamServer,
) error {
	// checkpoint data can be more than 4MB, it can cause stress
	// - on the client side (default limit on the receiving end)
	// - locally as the node already loads db query result in memory
	snapshot := types.LayerID(req.SnapshotLayer)
	numAtxs := int(req.NumAtxs)
	if numAtxs < defaultNumAtxs {
		numAtxs = defaultNumAtxs
	}
	err := checkpoint.Generate(stream.Context(), afero.NewOsFs(), a.db, a.dataDir, snapshot, numAtxs)
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("failed to create checkpoint: %s", err.Error()))
	}
	fname := checkpoint.SelfCheckpointFilename(a.dataDir, snapshot)
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}
	f, err := os.Open(fname)
	if err != nil {
		return status.Errorf(codes.Internal, fmt.Sprintf("failed to open file %s: %s", fname, err.Error()))
	}
	defer f.Close()
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

func (a AdminService) Recover(ctx context.Context, _ *pb.RecoverRequest) (*emptypb.Empty, error) {
	ctxzap.Info(ctx, "going to recover from checkpoint")
	a.recover()
	return &emptypb.Empty{}, nil
}

func (a AdminService) EventsStream(req *pb.EventStreamRequest, stream pb.AdminService_EventsStreamServer) error {
	sub, buf, err := events.SubscribeUserEvents(events.WithBuffer(1000))
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, err.Error())
	}
	defer sub.Close()
	// send empty header after subscribing to the channel.
	// this is optional but allows subscriber to wait until stream is fully initialized.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}
	buf.Iterate(func(ev events.UserEvent) bool {
		err = stream.Send(ev.Event)
		return err == nil
	})
	if err != nil {
		return fmt.Errorf("send buffered to stream: %w", err)
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-sub.Full():
			return status.Errorf(codes.Canceled, "buffer is full")
		case ev := <-sub.Out():
			if err := stream.Send(ev.Event); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		}
	}
}

func (a AdminService) PeerInfoStream(_ *emptypb.Empty, stream pb.AdminService_PeerInfoStreamServer) error {
	for _, p := range a.p.GetPeers() {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			info := a.p.ConnectedPeerInfo(p)
			// There is no guarantee that the peers originally returned will still
			// be connected by the time we call ConnectedPeerInfo.
			if info == nil {
				continue
			}
			connections := make([]*pb.ConnectionInfo, len(info.Connections))
			for j, c := range info.Connections {
				connections[j] = &pb.ConnectionInfo{
					Address:  c.Address.String(),
					Uptime:   durationpb.New(c.Uptime),
					Outbound: c.Outbound,
					Kind:     connKind(c.Kind),
				}
			}
			ds := info.DataStats
			msg := &pb.PeerInfo{
				Id:            info.ID.String(),
				Connections:   connections,
				Tags:          info.Tags,
				ClientStats:   peerStats(info.ClientStats),
				ServerStats:   peerStats(info.ServerStats),
				BytesSent:     uint64(ds.BytesSent),
				BytesReceived: uint64(ds.BytesReceived),
			}
			if ds.SendRate[0] != 0 || ds.SendRate[1] != 0 {
				msg.SendRate = []uint64{
					uint64(ds.SendRate[0]),
					uint64(ds.SendRate[1]),
				}
			}
			if ds.RecvRate[0] != 0 || ds.RecvRate[1] != 0 {
				msg.RecvRate = []uint64{
					uint64(ds.RecvRate[0]),
					uint64(ds.RecvRate[1]),
				}
			}
			err := stream.Send(msg)
			if err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		}
	}

	return nil
}

func connKind(kind peerinfo.Kind) pb.ConnectionInfo_Kind {
	switch kind {
	case peerinfo.KindInbound:
		return pb.ConnectionInfo_Inbound
	case peerinfo.KindOutbound:
		return pb.ConnectionInfo_Outbound
	case peerinfo.KindHolePunchInbound:
		return pb.ConnectionInfo_HPInbound
	case peerinfo.KindHolePunchOutbound:
		return pb.ConnectionInfo_HPOutbound
	case peerinfo.KindRelayInbound:
		return pb.ConnectionInfo_RelayInbound
	case peerinfo.KindRelayOutbound:
		return pb.ConnectionInfo_RelayOutbound
	default:
		return pb.ConnectionInfo_Uknown
	}
}

func peerStats(stats p2p.PeerRequestStats) *pb.PeerRequestStats {
	if stats.SuccessCount == 0 && stats.FailureCount == 0 {
		return nil
	}
	var latency *durationpb.Duration
	if stats.SuccessCount > 0 || stats.FailureCount > 0 {
		latency = durationpb.New(stats.Latency)
	}
	return &pb.PeerRequestStats{
		SuccessCount: uint64(stats.SuccessCount),
		FailureCount: uint64(stats.FailureCount),
		Latency:      latency,
	}
}
