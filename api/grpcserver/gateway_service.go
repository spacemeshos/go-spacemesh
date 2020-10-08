package grpcserver

import (
	"bytes"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GatewayService exposes transaction data, and a submit tx endpoint
type GatewayService struct {
	Network api.NetworkAPI // P2P Swarm
	Mesh    api.TxAPI      // Mesh
	Mempool api.MempoolAPI
	syncer  api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s GatewayService) RegisterService(server *Server) {
	pb.RegisterGatewayServiceServer(server.GrpcServer, s)
}

// NewGatewayService creates a new grpc service using config data.
func NewGatewayService(
	net api.NetworkAPI,
	tx api.TxAPI,
	mempool api.MempoolAPI,
	syncer api.Syncer,
) *GatewayService {
	return &GatewayService{
		Network: net,
		Mesh:    tx,
		Mempool: mempool,
		syncer:  syncer,
	}
}

// BroadcastPoet accepts a binary poet packet to broadcast to the network
func (s GatewayService) BroadcastPoet(ctx context.Context, in *pb.SubmitTransactionRequest) (*pb.SubmitTransactionResponse, error) {
	log.Info("GRPC GatewayService.SubmitTransaction")

	if len(in.Transaction) == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Transaction` payload empty")
	}

	if !s.syncer.IsSynced() {
		return nil, status.Error(codes.FailedPrecondition,
			"Cannot submit transaction, node is not in sync yet, try again later")
	}

	tx, err := types.BytesToTransaction(in.Transaction)
	if err != nil {
		log.Error("failed to deserialize tx, error: %v", err)
		return nil, status.Error(codes.InvalidArgument,
			"`Transaction` must contain a valid, serialized transaction")
	}
	log.Info("GRPC GatewayService.SubmitTransaction to address: %s (len: %v), "+
		"amount: %v, gaslimit: %v, fee: %v",
		tx.Recipient.String(), len(tx.Recipient), tx.Amount, tx.GasLimit, tx.Fee)
	if err := tx.CalcAndSetOrigin(); err != nil {
		log.Error("failed to calculate tx origin: %v", err)
		return nil, status.Error(codes.InvalidArgument,
			"`Transaction` must contain a valid, serialized transaction")
	}
	if !s.Mesh.AddressExists(tx.Origin()) {
		log.With().Error("tx origin address not found in global state",
			tx.ID(), log.String("origin", tx.Origin().Short()))
		return nil, status.Error(codes.InvalidArgument, "`Transaction` origin account not found")
	}
	if err := s.Mesh.ValidateNonceAndBalance(tx); err != nil {
		log.Error("tx failed nonce and balance check: %v", err)
		return nil, status.Error(codes.InvalidArgument, "`Transaction` incorrect counter or insufficient balance")
	}
	log.Info("GRPC GatewayService.SubmitTransaction BROADCAST tx address %x (len %v), gas limit %v, fee %v id %v nonce %v",
		tx.Recipient, len(tx.Recipient), tx.GasLimit, tx.Fee, tx.ID().ShortString(), tx.AccountNonce)
	go func() {
		if err := s.Network.Broadcast(state.IncomingTxProtocol, in.Transaction); err != nil {
			log.Error("error broadcasting incoming tx: %v", err)
		}
	}()

	return &pb.SubmitTransactionResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
		Txstate: &pb.TransactionState{
			Id:    &pb.TransactionId{Id: tx.ID().Bytes()},
			State: pb.TransactionState_TRANSACTION_STATE_MEMPOOL,
		},
	}, nil
}

