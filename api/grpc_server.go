// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"strconv"

	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// SpacemeshGrpcService is a grpc server providing the Spacemesh api
type SpacemeshGrpcService struct {
	Server *grpc.Server
	Port   uint
	StateApi StateAPI
	Network NetworkAPI
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) GetBalance(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	addr := address.BytesToAddress(in.Address)

	if s.StateApi.Exist(addr) != true {
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetBalance(addr)

	return &pb.SimpleMessage{Value:val.String()}, nil
}

// Echo returns the response for an echo api request
func (s SpacemeshGrpcService) GetNonce(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	addr := address.BytesToAddress(in.Address)

	if s.StateApi.Exist(addr)  != true{
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetNonce(addr)
	msg := pb.SimpleMessage{Value:strconv.FormatUint(val,10)}
	return &msg, nil
}

func (s SpacemeshGrpcService) SubmitTransaction(ctx context.Context, in *pb.SignedTransaction) (*pb.SimpleMessage, error) {
	const txGossipChannel = "txs" //todo: refactor this to actual channel name
	//todo: transactions should be syntactically validated

	//todo" should this be in a go routine?
	s.Network.Broadcast(txGossipChannel, in.TxData)

	return &pb.SimpleMessage{Value:"ok"}, nil
}

// P2P API

func (s SpacemeshGrpcService) Broadcast(ctx context.Context, in *pb.BroadcastMessage) (*pb.SimpleMessage, error) {
	err := s.Network.Broadcast(APIGossipProtocol, in.Data)
	if err != nil {
		log.Warning("RPC Broadcast failed please check that `test-mode` is on in order to use RPC Broadcast.")
		return &pb.SimpleMessage{Value:err.Error()}, err
	}
	return &pb.SimpleMessage{Value:"ok"}, nil
}


// StopService stops the grpc service.
func (s SpacemeshGrpcService) StopService() {
	log.Debug("Stopping grpc service...")
	s.Server.Stop()
	log.Debug("grpc service stopped...")

}

// NewGrpcService create a new grpc service using config data.
func NewGrpcService(net NetworkAPI ,state StateAPI) *SpacemeshGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &SpacemeshGrpcService{Server: server, Port: uint(port), StateApi:state, Network:net}
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
		log.Error("failed to listen err:%v", err)
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
		log.Errorw("grpc stopped serving", log.Err(err))
	}

	if status != nil {
		status <- true
	}

}
