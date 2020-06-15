package grpc

import (
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/poet/broadcaster/pb"
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

// NodeService is a grpc server providing the Spacemesh api
type Service struct {
	Server        *grpc.Server
	Port          uint
	StateAPI      api.StateAPI     // State DB
	Network       api.NetworkAPI   // P2P Swarm
	Tx            TxAPI            // Mesh
	TxMempool     *miner.TxMempool // TX Mempool
	Mining        api.MiningAPI    // ATX Builder
	Oracle        api.OracleAPI
	GenTime       api.GenesisTimeAPI
	Post          api.PostAPI
	LayerDuration time.Duration
	PeerCounter   PeerCounter
	Syncer        Syncer
	Config        *config.Config
	Logging       api.LoggingAPI
}

// StartService starts the grpc service.
func (s Service) StartService() {
	go s.startServiceInternal()
}

// This is a blocking method designed to be called using a go routine
func (s Service) startServiceInternal() {
	addr := ":" + strconv.Itoa(int(s.Port))

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen", err)
		return
	}

	pb.RegisterSpacemeshServiceServer(s.Server, s)

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s.Server)

	log.Info("grpc API listening on port %d", s.Port)

	// start serving - this blocks until err or server is stopped
	if err := s.Server.Serve(lis); err != nil {
		log.Error("grpc stopped serving", err)
	}

}

// Close stops the service.
func (s Service) Close() error {
	log.Debug("Stopping grpc service...")
	s.Server.Stop()
	log.Debug("grpc service stopped...")
	return nil
}

