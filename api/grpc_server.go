// NOTE: the contents of this file will soon be deprecated. See
// grpcserver/grpc.go for the new implementation.

// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
)

// SpacemeshGrpcService is a grpc server providing the Spacemesh api
type SpacemeshGrpcService struct {
	Server        *grpc.Server
	Port          uint
	StateAPI      StateAPI         // State DB
	Network       NetworkAPI       // P2P Swarm
	Tx            TxAPI            // Mesh
	TxMempool     *miner.TxMempool // TX Mempool
	Mining        MiningAPI        // ATX Builder
	Oracle        OracleAPI
	GenTime       GenesisTimeAPI
	Post          PostAPI
	LayerDuration time.Duration
	PeerCounter   PeerCounter
	Syncer        Syncer
	Config        *config.Config
	Logging       LoggingAPI
}

var _ pb.SpacemeshServiceServer = (*SpacemeshGrpcService)(nil)

func (s SpacemeshGrpcService) getTransactionAndStatus(txID types.TransactionID) (*types.Transaction, *types.LayerID, pb.TxStatus, error) {
	tx, err := s.Tx.GetTransaction(txID) // have we seen this transaction in a block?
	if err != nil {
		tx, err = s.TxMempool.Get(txID) // do we have it in the mempool?
		if err != nil {                 // we don't know this transaction
			return nil, nil, 0, fmt.Errorf("transaction not found, id: %s", util.Bytes2Hex(txID.Bytes()))
		}
		return tx, nil, pb.TxStatus_PENDING, nil
	}

	layerApplied := s.Tx.GetLayerApplied(txID)
	var status pb.TxStatus
	if layerApplied != nil {
		status = pb.TxStatus_CONFIRMED
	} else {
		nonce := s.StateAPI.GetNonce(tx.Origin())
		if nonce > tx.AccountNonce {
			status = pb.TxStatus_REJECTED
		} else {
			status = pb.TxStatus_PENDING
		}
	}
	return tx, layerApplied, status, nil
}

// GetTransaction gets transaction details by id
func (s SpacemeshGrpcService) GetTransaction(ctx context.Context, txID *pb.TransactionId) (*pb.Transaction, error) {
	id := types.TransactionID{}
	copy(id[:], txID.Id)

	tx, layerApplied, status, err := s.getTransactionAndStatus(id)
	if err != nil {
		return nil, err
	}

	var layerID, timestamp uint64
	if layerApplied != nil {
		layerID = uint64(*layerApplied)
		timestamp = uint64(s.GenTime.GetGenesisTime().Add(s.LayerDuration * time.Duration(layerID+1)).Unix())
		// We use layerID + 1 so the timestamp is the end of the layer.
	}

	return &pb.Transaction{
		TxId: txID,
		Sender: &pb.AccountId{
			Address: util.Bytes2Hex(tx.Origin().Bytes()),
		},
		Receiver: &pb.AccountId{
			Address: util.Bytes2Hex(tx.Recipient.Bytes()),
		},
		Amount:    tx.Amount,
		Fee:       tx.Fee,
		Status:    status,
		LayerId:   layerID,
		Timestamp: timestamp,
	}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

func (s SpacemeshGrpcService) getProjection(addr types.Address) (nonce, balance uint64, err error) {
	nonce = s.StateAPI.GetNonce(addr)
	balance = s.StateAPI.GetBalance(addr)
	nonce, balance, err = s.Tx.GetProjection(addr, nonce, balance)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = s.TxMempool.GetProjection(addr, nonce, balance)
	return nonce, balance, nil
}

// GetBalance returns the current account balance for the provided account ID. The balance is based on the global state
// and all known transactions in unapplied blocks and the mempool that originate from the given account. Unapplied
// transactions coming INTO the given account (from mempool or unapplied blocks) are NOT counted.
func (s SpacemeshGrpcService) GetBalance(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Debug("GRPC GetBalance msg")
	addr := types.HexToAddress(in.Address)
	log.Debug("GRPC GetBalance for address %x (len %v)", addr, len(addr))
	if s.StateAPI.Exist(addr) != true {
		log.Error("GRPC GetBalance returned error msg: account does not exist, address %x", addr)
		return nil, fmt.Errorf("account does not exist")
	}

	_, balance, err := s.getProjection(addr)
	if err != nil {
		return nil, err
	}
	msg := &pb.SimpleMessage{Value: strconv.FormatUint(balance, 10)}
	log.Debug("GRPC GetBalance returned msg.Value %v", msg.Value)
	return msg, nil
}

// GetNonce returns the current account nonce for the provided account ID. The nonce is based on the global state and
// all known transactions in unapplied blocks and the mempool.
func (s SpacemeshGrpcService) GetNonce(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetNonce msg")
	addr := types.HexToAddress(in.Address)

	if s.StateAPI.Exist(addr) != true {
		log.Error("GRPC GetNonce got error msg: account does not exist, %v", addr)
		return nil, fmt.Errorf("account does not exist")
	}

	nonce, _, err := s.getProjection(addr)
	if err != nil {
		return nil, err
	}
	msg := &pb.SimpleMessage{Value: strconv.FormatUint(nonce, 10)}
	log.Info("GRPC GetNonce returned msg.Value %v", msg.Value)
	return msg, nil
}

// SubmitTransaction transmits transaction via gossip metwork
func (s SpacemeshGrpcService) SubmitTransaction(ctx context.Context, in *pb.SignedTransaction) (*pb.TxConfirmation, error) {
	log.Info("GRPC SubmitTransaction msg")

	tx, err := types.BytesToTransaction(in.Tx)
	if err != nil {
		log.Error("failed to deserialize tx, error %v", err)
		return nil, err
	}
	log.Info("GRPC SubmitTransaction to address: %s (len: %v), amount: %v gaslimit: %v, fee: %v",
		tx.Recipient.Short(), len(tx.Recipient), tx.Amount, tx.GasLimit, tx.Fee)
	if err := tx.CalcAndSetOrigin(); err != nil {
		log.With().Error("failed to calc origin", log.Err(err))
		return nil, err
	}
	if !s.Tx.AddressExists(tx.Origin()) {
		log.With().Error("tx failed to validate signature",
			tx.ID(), log.String("origin", tx.Origin().Short()))
		return nil, fmt.Errorf("transaction origin (%v) not found in global state", tx.Origin().Short())
	}
	if err := s.Tx.ValidateNonceAndBalance(tx); err != nil {
		log.With().Error("tx failed nonce and balance check", log.Err(err))
		return nil, err
	}
	log.Info("GRPC SubmitTransaction BROADCAST tx. address %x (len %v), gas limit %v, fee %v id %v nonce %v",
		tx.Recipient, len(tx.Recipient), tx.GasLimit, tx.Fee, tx.ID().ShortString(), tx.AccountNonce)
	go s.Network.Broadcast(miner.IncomingTxProtocol, in.Tx)
	log.Info("GRPC SubmitTransaction returned msg ok")
	return &pb.TxConfirmation{Value: "ok", Id: hex.EncodeToString(tx.ID().Bytes())}, nil
}

// P2P API

// Broadcast broadcasts message to gossip network
func (s SpacemeshGrpcService) Broadcast(ctx context.Context, in *pb.BroadcastMessage) (*pb.SimpleMessage, error) {
	log.Info("GRPC Broadcast msg")
	err := s.Network.Broadcast(apiGossipProtocol, []byte(in.Data))
	if err != nil {
		log.Warning("RPC Broadcast failed please check that `test-mode` is on in order to use RPC Broadcast.")
		return &pb.SimpleMessage{Value: err.Error()}, err
	}
	log.Info("GRPC Broadcast msg ok")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// BroadcastPoet is a specialized API to broadcast poet messages
func (s SpacemeshGrpcService) BroadcastPoet(ctx context.Context, in *pb.BinaryMessage) (*pb.SimpleMessage, error) {
	log.Debug("GRPC Broadcast PoET msg")
	err := s.Network.Broadcast(activation.PoetProofProtocol, in.Data)
	if err != nil {
		log.Error("failed to broadcast PoET message: %v", err.Error())
		return &pb.SimpleMessage{Value: err.Error()}, err
	}
	log.Debug("PoET message broadcast succeeded")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// Close stops the grpc service.
func (s SpacemeshGrpcService) Close() error {
	log.Debug("Stopping grpc service...")
	s.Server.Stop()
	log.Debug("grpc service stopped...")
	return nil
}

// NewGrpcService create a new grpc service using config data.
func NewGrpcService(port int, net NetworkAPI, state StateAPI, tx TxAPI, txMempool *miner.TxMempool, mining MiningAPI, oracle OracleAPI, genTime GenesisTimeAPI, post PostAPI, layerDurationSec int, syncer Syncer, cfg *config.Config, logging LoggingAPI) *SpacemeshGrpcService {
	options := []grpc.ServerOption{
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
	server := grpc.NewServer(options...)
	return &SpacemeshGrpcService{
		Server:        server,
		Port:          uint(port),
		StateAPI:      state,
		Network:       net,
		Tx:            tx,
		TxMempool:     txMempool,
		Mining:        mining,
		Oracle:        oracle,
		GenTime:       genTime,
		Post:          post,
		LayerDuration: time.Duration(layerDurationSec) * time.Second,
		PeerCounter:   peers.NewPeers(net, log.NewDefault("grpc")),
		Syncer:        syncer,
		Config:        cfg,
		Logging:       logging,
	}
}

// StartService starts the grpc service.
func (s SpacemeshGrpcService) StartService() {
	go s.startServiceInternal()
}

// This is a blocking method designed to be called using a go routine
func (s SpacemeshGrpcService) startServiceInternal() {
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

// StartMining start post init followed by publication of atxs and blocks
func (s SpacemeshGrpcService) StartMining(ctx context.Context, message *pb.InitPost) (*pb.SimpleMessage, error) {
	log.Info("GRPC StartMining msg")
	addr, err := types.StringToAddress(message.Coinbase)
	if err != nil {
		return nil, err
	}
	err = s.Mining.StartPost(addr, message.LogicalDrive, message.CommitmentSize)
	if err != nil {
		return nil, err
	}
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// SetAwardsAddress sets the award address for which this miner will receive awards
func (s SpacemeshGrpcService) SetAwardsAddress(ctx context.Context, id *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetAwardsAddress msg")
	addr := types.HexToAddress(id.Address)
	s.Mining.SetCoinbaseAccount(addr)
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// GetMiningStats returns post creation status, coinbase address and post data directory
func (s SpacemeshGrpcService) GetMiningStats(ctx context.Context, empty *empty.Empty) (*pb.MiningStats, error) {
	//todo: we should review if this RPC is necessary
	log.Info("GRPC GetInitProgress msg")
	stat, remainingBytes, coinbase, dataDir := s.Mining.MiningStats()
	return &pb.MiningStats{
		DataDir:        dataDir,
		Status:         int32(stat),
		Coinbase:       coinbase,
		RemainingBytes: remainingBytes,
	}, nil
}

// GetNodeStatus returns a status object providing information about the connected peers, sync status,
// current and verified layer
func (s SpacemeshGrpcService) GetNodeStatus(context.Context, *empty.Empty) (*pb.NodeStatus, error) {
	return &pb.NodeStatus{
		Peers:         s.PeerCounter.PeerCount(),
		MinPeers:      uint64(s.Config.P2P.SwarmConfig.RandomConnections),
		MaxPeers:      uint64(s.Config.P2P.MaxInboundPeers + s.Config.P2P.SwarmConfig.RandomConnections),
		Synced:        s.Syncer.IsSynced(),
		SyncedLayer:   s.Tx.LatestLayer().Uint64(),
		CurrentLayer:  s.GenTime.GetCurrentLayer().Uint64(),
		VerifiedLayer: s.Tx.LatestLayerInState().Uint64(),
	}, nil
}

// GetUpcomingAwards returns the id of layers at which this miner will receive rewards
func (s SpacemeshGrpcService) GetUpcomingAwards(ctx context.Context, empty *empty.Empty) (*pb.EligibleLayers, error) {
	log.Info("GRPC GetUpcomingAwards msg")
	layers := s.Oracle.GetEligibleLayers()
	ly := make([]uint64, 0, len(layers))
	for _, l := range layers {
		ly = append(ly, uint64(l))
	}
	return &pb.EligibleLayers{Layers: ly}, nil
}

// GetGenesisTime returns the time at which this blockmesh has started
func (s SpacemeshGrpcService) GetGenesisTime(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetGenesisTime msg")
	return &pb.SimpleMessage{Value: s.GenTime.GetGenesisTime().Format(time.RFC3339)}, nil
}

// ResetPost removed post commitment for this miner
func (s SpacemeshGrpcService) ResetPost(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC ResetPost msg")
	stat, _, _, _ := s.Mining.MiningStats()
	if stat == activation.InitInProgress {
		return nil, fmt.Errorf("cannot reset, init in progress")
	}
	err := s.Post.Reset()
	if err != nil {
		return nil, err
	}
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// SetLoggerLevel sets logger level for specific logger
func (s SpacemeshGrpcService) SetLoggerLevel(ctx context.Context, msg *pb.SetLogLevel) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetLogLevel msg")
	err := s.Logging.SetLogLevel(msg.LoggerName, msg.Severity)
	if err != nil {
		return nil, err
	}
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// GetAccountTxs returns transactions that were applied and pending by this account
func (s SpacemeshGrpcService) GetAccountTxs(ctx context.Context, txsSinceLayer *pb.GetTxsSinceLayer) (*pb.AccountTxs, error) {
	log.Debug("GRPC GetAccountTxs msg")

	currentPBase := s.Tx.LatestLayerInState()

	addr := types.HexToAddress(txsSinceLayer.Account.Address)
	minLayer := types.LayerID(txsSinceLayer.StartLayer)
	if minLayer > s.Tx.LatestLayer() {
		return &pb.AccountTxs{}, fmt.Errorf("invalid start layer")
	}

	txs := pb.AccountTxs{ValidatedLayer: currentPBase.Uint64()}

	meshTxIds := s.getTxIdsFromMesh(minLayer, addr)
	for _, txID := range meshTxIds {
		txs.Txs = append(txs.Txs, txID.String())
	}

	mempoolTxIds := s.TxMempool.GetTxIdsByAddress(addr)
	for _, txID := range mempoolTxIds {
		txs.Txs = append(txs.Txs, txID.String())
	}

	return &txs, nil
}

func (s SpacemeshGrpcService) getTxIdsFromMesh(minLayer types.LayerID, addr types.Address) []types.TransactionID {
	var txIDs []types.TransactionID
	for layerID := minLayer; layerID < s.Tx.LatestLayer(); layerID++ {
		destTxIDs := s.Tx.GetTransactionsByDestination(layerID, addr)
		txIDs = append(txIDs, destTxIDs...)
		originTxIds := s.Tx.GetTransactionsByOrigin(layerID, addr)
		txIDs = append(txIDs, originTxIds...)
	}
	return txIDs
}

// GetAccountRewards returns the rewards for the provided account
func (s SpacemeshGrpcService) GetAccountRewards(ctx context.Context, account *pb.AccountId) (*pb.AccountRewards, error) {
	log.Debug("GRPC GetAccountRewards msg")
	acc := types.HexToAddress(account.Address)

	rewards, err := s.Tx.GetRewards(acc)
	if err != nil {
		log.Error("failed to get rewards: %v", err)
		return nil, err
	}
	rewardsOut := pb.AccountRewards{}
	for _, x := range rewards {
		rewardsOut.Rewards = append(rewardsOut.Rewards, &pb.Reward{
			Layer:               x.Layer.Uint64(),
			TotalReward:         x.TotalReward,
			LayerRewardEstimate: x.LayerRewardEstimate,
		})
	}

	return &rewardsOut, nil
}

// GetStateRoot returns current state root
func (s SpacemeshGrpcService) GetStateRoot(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetStateRoot msg")
	return &pb.SimpleMessage{Value: s.Tx.GetStateRoot().String()}, nil
}
