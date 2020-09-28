package grpcserver

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NodeService is a grpc server that provides the NodeService, which exposes node-related
// data such as node status, software version, errors, etc. It can also be used to start
// the sync process, or to shut down the node.
type NodeService struct {
	Mesh        api.TxAPI
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer

	log log.Logger
}

// RegisterService registers this service with a grpc server instance
func (s NodeService) RegisterService(server *Server) {
	pb.RegisterNodeServiceServer(server.GrpcServer, s)
}

// NodeServiceOption type defines a callback that can set internal NodeService fields
type NodeServiceOption func(n *NodeService)

// WithNodeServiceLogger set's the underlying logger to a custom logger.
// By default the logger is NoOp
func WithNodeServiceLogger(log log.Logger) NodeServiceOption {
	return func(n *NodeService) {
		n.log = log
	}
}

// NewNodeService creates a new grpc service using config data.
func NewNodeService(
	net api.NetworkAPI, tx api.TxAPI,
	genTime api.GenesisTimeAPI,
	syncer api.Syncer,
	options ...NodeServiceOption,
) *NodeService {
	n := &NodeService{
		Mesh:        tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpcserver.NodeService")),
		Syncer:      syncer,
		log:         log.NewNop(),
	}
	for _, option := range options {
		option(n)
	}
	return n
}

// Echo returns the response for an echo api request. It's used for E2E tests.
func (s NodeService) Echo(ctx context.Context, in *pb.EchoRequest) (*pb.EchoResponse, error) {
	s.log.Info("GRPC NodeService.Echo")
	if in.Msg != nil {
		return &pb.EchoResponse{Msg: &pb.SimpleString{Value: in.Msg.Value}}, nil
	}
	return nil, status.Errorf(codes.InvalidArgument, "Must include `Msg`")
}

// Version returns the version of the node software as a semver string
func (s NodeService) Version(ctx context.Context, in *empty.Empty) (*pb.VersionResponse, error) {
	s.log.Info("GRPC NodeService.Version")
	return &pb.VersionResponse{
		VersionString: &pb.SimpleString{Value: cmd.Version},
	}, nil
}

// Build returns the build of the node software
func (s NodeService) Build(ctx context.Context, in *empty.Empty) (*pb.BuildResponse, error) {
	s.log.Info("GRPC NodeService.Build")
	return &pb.BuildResponse{
		BuildString: &pb.SimpleString{Value: cmd.Commit},
	}, nil
}

// Status returns a status object providing information about the connected peers, sync status,
// current and verified layer
func (s NodeService) Status(ctx context.Context, request *pb.StatusRequest) (*pb.StatusResponse, error) {
	s.log.Info("GRPC NodeService.Status")
	return &pb.StatusResponse{
		Status: &pb.NodeStatus{
			ConnectedPeers: s.PeerCounter.PeerCount(),            // number of connected peers
			IsSynced:       s.Syncer.IsSynced(),                  // whether the node is synced
			SyncedLayer:    s.Mesh.LatestLayer().Uint64(),        // latest layer we saw from the network
			TopLayer:       s.GenTime.GetCurrentLayer().Uint64(), // current layer, based on time
			VerifiedLayer:  s.Mesh.LatestLayerInState().Uint64(), // latest verified layer
		},
	}, nil
}

// SyncStart requests that the node start syncing the mesh (if it isn't already syncing)
func (s NodeService) SyncStart(ctx context.Context, request *pb.SyncStartRequest) (*pb.SyncStartResponse, error) {
	s.log.Info("GRPC NodeService.SyncStart")
	s.Syncer.Start()
	return &pb.SyncStartResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// Shutdown requests a graceful shutdown
func (s NodeService) Shutdown(ctx context.Context, request *pb.ShutdownRequest) (*pb.ShutdownResponse, error) {
	s.log.Info("GRPC NodeService.Shutdown")
	cmd.Cancel()
	return &pb.ShutdownResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// STREAMS

// StatusStream exposes a stream of node status updates
func (s NodeService) StatusStream(request *pb.StatusStreamRequest, stream pb.NodeService_StatusStreamServer) error {
	s.log.Info("GRPC NodeService.StatusStream")
	statusStream := events.GetStatusChannel()

	for {
		select {
		case _, ok := <-statusStream:
			// statusStream works a bit differently than the other streams. It doesn't actually
			// send us data. Instead, it just notifies us that there's new data to be read.
			if !ok {
				s.log.Info("StatusStream closed, shutting down")
				return nil
			}
			if err := stream.Send(&pb.StatusStreamResponse{
				Status: &pb.NodeStatus{
					ConnectedPeers: s.PeerCounter.PeerCount(),            // number of connected peers
					IsSynced:       s.Syncer.IsSynced(),                  // whether the node is synced
					SyncedLayer:    s.Mesh.LatestLayer().Uint64(),        // latest layer we saw from the network
					TopLayer:       s.GenTime.GetCurrentLayer().Uint64(), // current layer, based on time
					VerifiedLayer:  s.Mesh.LatestLayerInState().Uint64(), // latest verified layer
				},
			}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			s.log.Info("StatusStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// ErrorStream exposes a stream of node errors
func (s NodeService) ErrorStream(request *pb.ErrorStreamRequest, stream pb.NodeService_ErrorStreamServer) error {
	s.log.Info("GRPC NodeService.ErrorStream")
	errorStream := events.GetErrorChannel()

	for {
		select {
		case nodeError, ok := <-errorStream:
			if !ok {
				s.log.Info("ErrorStream closed, shutting down")
				return nil
			}
			if err := stream.Send(&pb.ErrorStreamResponse{Error: &pb.NodeError{
				ErrorType:  convertErrorType(nodeError.Type),
				Message:    nodeError.Msg,
				StackTrace: nodeError.Trace,
			}}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			s.log.Info("ErrorStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

func convertErrorType(errType int) pb.NodeError_NodeErrorType {
	switch errType {
	case events.NodeErrorTypePanic:
		return pb.NodeError_NODE_ERROR_TYPE_PANIC
	case events.NodeErrorTypePanicHare:
		return pb.NodeError_NODE_ERROR_TYPE_PANIC_HARE
	case events.NodeErrorTypePanicSync:
		return pb.NodeError_NODE_ERROR_TYPE_PANIC_SYNC
	case events.NodeErrorTypePanicP2P:
		return pb.NodeError_NODE_ERROR_TYPE_PANIC_P2P
	case events.NodeErrorTypeSignalShutdown:
		return pb.NodeError_NODE_ERROR_TYPE_SIGNAL_SHUT_DOWN
	default:
		return pb.NodeError_NODE_ERROR_TYPE_UNSPECIFIED
	}
}
