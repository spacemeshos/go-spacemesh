// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/types"
	"math/big"
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
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) GetBalance(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetBalance msg")
	addr := address.HexToAddress(in.Address)

	if s.StateApi.Exist(addr) != true {
		log.Error("GRPC GetBalance returned error msg: account does not exist")
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetBalance(addr)
	log.Info("GRPC GetBalance returned msg %v", val.String())
	return &pb.SimpleMessage{Value: val.String()}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) GetNonce(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetNonce msg")
	addr := address.HexToAddress(in.Address)

	if s.StateApi.Exist(addr) != true {
		log.Error("GRPC GetNonce got error msg: account does not exist")
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetNonce(addr)
	msg := pb.SimpleMessage{Value: strconv.FormatUint(val, 10)}
	log.Error("GRPC GetNonce returned msg %v", strconv.FormatUint(val, 10))
	return &msg, nil
}

func (s SpacemeshGrpcService) SubmitTransaction(ctx context.Context, in *pb.SignedTransaction) (*pb.SimpleMessage, error) {
	log.Info("GRPC SubmitTransaction msg")
	tx := types.SerializableTransaction{}
	addr := address.HexToAddress(in.DstAddress)
	tx.Recipient = &addr
	tx.Origin = address.HexToAddress(in.SrcAddress)

	num, _ := strconv.ParseInt(in.Nonce, 10, 64)
	tx.AccountNonce = uint64(num)
	amount := big.Int{}
	amount.SetString(in.Amount, 10)
	tx.Amount = amount.Bytes()
	tx.GasLimit = 10
	tx.Price = big.NewInt(10).Bytes()

	val, err := types.TransactionAsBytes(&tx)
	if err != nil {
		return nil, err
	}

	//todo" should this be in a go routine?
	s.Network.Broadcast(miner.IncomingTxProtocol, val)
	log.Info("GRPC SubmitTransaction returned msg ok")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// P2P API

func (s SpacemeshGrpcService) Broadcast(ctx context.Context, in *pb.BroadcastMessage) (*pb.SimpleMessage, error) {
	log.Info("GRPC Broadcast msg")
	err := s.Network.Broadcast(APIGossipProtocol, []byte(in.Data))
	if err != nil {
		log.Warning("RPC Broadcast failed please check that `test-mode` is on in order to use RPC Broadcast.")
		return &pb.SimpleMessage{Value: err.Error()}, err
	}
	log.Info("GRPC Broadcast msg ok")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

func (s SpacemeshGrpcService) BroadcastPoet(ctx context.Context, in *pb.BinaryMessage) (*pb.SimpleMessage, error) {
	log.Info("GRPC Broadcast PoET msg")
	err := s.Network.Broadcast(activation.PoetProofProtocol, in.Data)
	if err != nil {
		log.Warning("failed to broadcast PoET message")
		return &pb.SimpleMessage{Value: err.Error()}, err
	}
	log.Info("PoET message broadcast succeeded")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

// StopService stops the grpc service.
func (s SpacemeshGrpcService) StopService() {
	log.Debug("Stopping grpc service...")
	s.Server.Stop()
	log.Debug("grpc service stopped...")

}

// NewGrpcService create a new grpc service using config data.
func NewGrpcService(net NetworkAPI, state StateAPI) *SpacemeshGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &SpacemeshGrpcService{Server: server, Port: uint(port), StateApi: state, Network: net}
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

func (s SpacemeshGrpcService) SetCommitmentSize(ctx context.Context, message *pb.CommitmentSizeMessage) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetCommitmentSize msg")

	return &pb.SimpleMessage{Value: "ok"}, nil
}

func (s SpacemeshGrpcService) SetLogicalDrive(ctx context.Context, message *pb.LogicalDriveMessage) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetLogicalDrive msg")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

func (s SpacemeshGrpcService) SetAwardsAddress(ctx context.Context, id *pb.AccountId) (*pb.SimpleMessage, error) {
	log.Info("GRPC SetAwardsAddress msg")
	return &pb.SimpleMessage{Value: "ok"}, nil
}

func (s SpacemeshGrpcService) GetInitProgress(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetInitProgress msg")
	return &pb.SimpleMessage{Value: "80"}, nil
}

func (s SpacemeshGrpcService) GetTotalAwards(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetTotalAwards msg")
	return &pb.SimpleMessage{Value: "1234"}, nil
}

func (s SpacemeshGrpcService) GetUpcomingAwards(ctx context.Context, empty *empty.Empty) (*pb.SimpleMessage, error) {
	log.Info("GRPC GetUpcomingAwards msg")
	return &pb.SimpleMessage{Value: "43221"}, nil
}
