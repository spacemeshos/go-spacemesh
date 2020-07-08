package grpcserver

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
)

// TransactionService exposes transaction data, and a submit tx endpoint
type TransactionService struct {
	Network     api.NetworkAPI // P2P Swarm
	Tx          api.TxAPI      // Mesh
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s TransactionService) RegisterService(server *Server) {
	pb.RegisterTransactionServiceServer(server.GrpcServer, s)
}

// NewGlobalStateService creates a new grpc service using config data.
func NewTransactionService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer) *TransactionService {
	return &TransactionService{
		Network:     net,
		Tx:          tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpc_server.TransactionService")),
		Syncer:      syncer,
	}
}

// SubmitTransaction allows a new tx to be submitted
func (s TransactionService) SubmitTransaction(ctx context.Context, in *pb.SubmitTransactionRequest) (*pb.SubmitTransactionResponse, error) {
	log.Info("GRPC TransactionService.SubmitTransaction")
	return nil, nil
}

// TransactionsState returns current tx data for one or more txs
func (s TransactionService) TransactionsState(ctx context.Context, in *pb.TransactionsStateRequest) (*pb.TransactionsStateResponse, error) {
	log.Info("GRPC TransactionService.TransactionsState")
	return nil, nil
}

// STREAMS

// TransactionsStateStream exposes a stream of tx data
func (s TransactionService) TransactionsStateStream(request *pb.TransactionsStateStreamRequest, stream pb.TransactionService_TransactionsStateStreamServer) error {
	log.Info("GRPC TransactionService.TransactionsStateStream")
	return nil
}
