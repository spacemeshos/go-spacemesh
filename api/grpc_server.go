// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"encoding/hex"
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/types"
	"net"
	"strconv"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// SpacemeshGrpcService is a grpc server providing the Spacemesh api
type SpacemeshGrpcService struct {
	Server   *grpc.Server
	Port     uint
	StateApi StateAPI
	Network  NetworkAPI
	Tx       TxAPI
	Mining   MiningAPI
	Oracle   OracleAPI
	GenTime  GenesisTimeAPI
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) GetBalance(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetBalance msg")
	addr := address.HexToAddress(in.Address)
	log.Info("GRPC GetBalance for address %x (len %v)", addr, len(addr))
	if s.StateApi.Exist(addr) != true {
		log.Error("GRPC GetBalance returned error msg: account does not exist, address %x", addr)
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetBalance(addr)
	valStr := strconv.FormatUint(val, 10)
	log.Info("GRPC GetBalance returned msg %v", valStr)
	return &pb.SimpleMessage{Value: valStr}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) GetNonce(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetNonce msg")
	addr := address.HexToAddress(in.Address)

	if s.StateApi.Exist(addr) != true {
		log.Error("GRPC GetNonce got error msg: account does not exist, %v", addr)
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetNonce(addr)
	msg := pb.SimpleMessage{Value: strconv.FormatUint(val, 10)}
	log.Info("GRPC GetNonce returned msg %v", strconv.FormatUint(val, 10))
	return &msg, nil
}

func (s SpacemeshGrpcService) SubmitTransaction(ctx context.Context, in *pb.SignedTransaction) (*pb.TxConfirmation, error) {
	log.Info("GRPC SubmitTransaction msg")

	signedTx := &types.SerializableSignedTransaction{}
	err := types.BytesToInterface(in.Tx, signedTx)
	if err != nil {
		log.Error("failed to deserialize tx, error %v", err)
		return nil, err
	}
	log.Info("GRPC SubmitTransaction to address: %s (len: %v), amount: %v gaslimit: %v, price: %v", signedTx.Recipient.Short(), len(signedTx.Recipient), signedTx.Amount, signedTx.GasLimit, signedTx.GasPrice)
	_, err = s.Tx.ValidateTransactionSignature(signedTx)
	if err != nil {
		log.Error("tx failed to validate signature, error %v", err)
		return nil, err
	}
	id := types.GetTransactionId(signedTx)
	hexIdStr := hex.EncodeToString(id[:])
	log.Info("GRPC SubmitTransaction BROADCAST tx. address %x (len %v), gaslimit %v, price %v id %v", signedTx.Recipient, len(signedTx.Recipient), signedTx.GasLimit, signedTx.GasPrice, hexIdStr[:5])
	go s.Network.Broadcast(miner.IncomingTxProtocol, in.Tx)
	log.Info("GRPC SubmitTransaction returned msg ok")
	return &pb.TxConfirmation{Value: "ok", Id: hexIdStr}, nil
}

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

// StopService stops the grpc service.
func (s SpacemeshGrpcService) StopService() {
	log.Debug("Stopping grpc service...")
	s.Server.Stop()
	log.Debug("grpc service stopped...")

}

type TxAPI interface {
	ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error)
}

// NewGrpcService create a new grpc service using config data.
func NewGrpcService(net NetworkAPI, state StateAPI, tx TxAPI, mining MiningAPI, oracle OracleAPI, genTime GenesisTimeAPI) *SpacemeshGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &SpacemeshGrpcService{Server: server,
		Port:     uint(port),
		StateApi: state,
		Network:  net,
		Tx:       tx,
		Mining:   mining,
		Oracle:   oracle,
		GenTime:  genTime,
	}
}

// StartService starts the grpc service.
func (s SpacemeshGrpcService) StartService(status chan bool) {
	go s.startServiceInternal(status)
}

// This is a blocking method designed to be called using a go routine
func (s SpacemeshGrpcService) startServiceInternal(status chan bool) {
	port := config.ConfigValues.GrpcServerPort
	addr := ":" + strconv.Itoa(int(port))

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen", err)
		return
	}

	pb.RegisterSpacemeshServiceServer(s.Server, s)

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s.Server)

	log.Debug("grpc API listening on port %d", port)

	if status != nil {
		status <- true
	}

	// start serving - this blocks until err or server is stopped
	if err := s.Server.Serve(lis); err != nil {
		log.Error("grpc stopped serving", err)
	}

	if status != nil {
		status <- true
	}

}

func (s SpacemeshGrpcService) StartMining(ctx context.Context, message *pb.InitPost) (*pb.SimpleMessage, error) {
	log.Info("GRPC StartMining msg")
	addr, err := address.StringToAddress(message.Coinbase)
	if err != nil {
		return nil, err
	}
	err = s.Mining.StartPost(addr, message.LogicalDrive, message.CommitmentSize)
	if err != nil {
		return nil, err
	}
	return &pb.SimpleMessage{Value: "ok"}, nil
}

func (s SpacemeshGrpcService) SetAwardsAddress(ctx context.Context, id *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetAwardsAddress msg")
	addr, err := address.StringToAddress(id.Address)
	if err != nil {
		return &pb.SimpleMessage{}, err
	}
	s.Mining.SetCoinbaseAccount(addr)
	return &pb.SimpleMessage{Value: "ok"}, nil
}

func (s SpacemeshGrpcService) GetMiningStats(ctx context.Context, empty *empty.Empty) (*pb.MiningStats, error) {
	//todo: we should review if this RPC is necessary
	log.Info("GRPC GetInitProgress msg")
	stat, coinbase, dataDir := s.Mining.MiningStats()
	return &pb.MiningStats{Status: int32(stat), Coinbase: coinbase, DataDir: dataDir}, nil
}

func (s SpacemeshGrpcService) GetTotalAwards(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	//todo: we should review if this RPC is necessary
	log.Info("GRPC GetTotalAwards msg")
	return &pb.SimpleMessage{Value: "1234"}, nil
}

func (s SpacemeshGrpcService) GetUpcomingAwards(ctx context.Context, empty *empty.Empty) (*pb.EligibleLayers, error) {
	log.Info("GRPC GetUpcomingAwards msg")
	layers := s.Oracle.GetEligibleLayers()
	ly := make([]uint64, 0, len(layers))
	for _, l := range layers {
		ly = append(ly, uint64(l))
	}
	return &pb.EligibleLayers{Layers: ly}, nil
}

func (s SpacemeshGrpcService) GetGenesisTime(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetGenesisTime msg")
	return &pb.SimpleMessage{Value: s.GenTime.GetGenesisTime().String()}, nil
}
