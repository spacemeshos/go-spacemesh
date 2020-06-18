package grpc_server

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"net"
	"strconv"
	"time"
)

// PeerCounter is an api to get amount of connected peers
type PeerCounter interface {
	PeerCount() uint64
}

// TxAPI is an api for getting transaction transaction status
type TxAPI interface {
	AddressExists(addr types.Address) bool
	ValidateNonceAndBalance(transaction *types.Transaction) error
	GetRewards(account types.Address) (rewards []types.Reward, err error)
	GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionID)
	GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionID)
	LatestLayer() types.LayerID
	GetLayerApplied(txID types.TransactionID) *types.LayerID
	GetTransaction(id types.TransactionID) (*types.Transaction, error)
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
	LatestLayerInState() types.LayerID
	GetStateRoot() types.Hash32
}

// ServiceStarter is an interface that knits together the various grpc service servers,
// allowing the glue code in this file to manage them all.
type ServiceServer interface {
	Server() *grpc.Server
	Port() uint
	registerService()
	Close() error
}

// Service is a barebones struct that all service servers embed. It stores properties
// used by all service servers.
type Service struct {
	server *grpc.Server
	port   uint
}

func (s Service) Server() *grpc.Server { return s.server }
func (s Service) Port() uint           { return s.port }

var ServerOptions = []grpc.ServerOption{
	// XXX: this is done to prevent routers from cleaning up our connections (e.g aws load balances..)
	// TODO: these parameters work for now but we might need to revisit or add them as configuration
	// TODO: Configure maxconns, maxconcurrentcons ..
	grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle:     time.Minute * 120,
		MaxConnectionAge:      time.Minute * 180,
		MaxConnectionAgeGrace: time.Minute * 10,
		Time:                  time.Minute,
		Timeout:               time.Minute * 3,
	}),
}

// StartService starts the grpc service.
func StartService(s ServiceServer) {
	go startServiceInternal(s)
}

// This is a blocking method designed to be called using a go routine
func startServiceInternal(s ServiceServer) {
	addr := ":" + strconv.Itoa(int(s.Port()))

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen", err)
		return
	}

	s.registerService()

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s.Server())

	log.Info("grpc API listening on port %d", s.Port())

	// start serving - this blocks until err or server is stopped
	if err := s.Server().Serve(lis); err != nil {
		log.Error("grpc server stopped", err)
	}

}

// Close stops the service.
func (s Service) Close() error {
	log.Debug("Stopping grpc service...")
	s.server.Stop()
	log.Debug("grpc service stopped...")
	return nil
}
