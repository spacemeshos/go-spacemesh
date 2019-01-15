// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"strconv"

	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// SpaceMeshGrpcService is a grpc server providing the Spacemesh api
type SpaceMeshGrpcService struct {
	Server *grpc.Server
	Port   uint
	StateApi StateAPI
}

// Echo returns the response for an echo api request
func (s SpaceMeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

// Echo returns the response for an echo api request
func (s SpaceMeshGrpcService) GetBalance(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	addr := common.BytesToAddress(in.Address)

	if s.StateApi.Exist(addr) != true {
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetBalance(addr)

	return &pb.SimpleMessage{Value:val.String()}, nil
}

// Echo returns the response for an echo api request
func (s SpaceMeshGrpcService) GetNonce(ctx context.Context, in *pb.AccountId) (*pb.SimpleMessage, error) {
	addr := common.BytesToAddress(in.Address)

	if s.StateApi.Exist(addr)  != true{
		return nil, fmt.Errorf("account does not exist")
	}

	val := s.StateApi.GetNonce(addr)
	msg := pb.SimpleMessage{Value:strconv.FormatUint(val,10)}
	return &msg, nil
}

// Echo returns the response for an echo api request
/*func (s SpaceMeshGrpcService) TransferFunds(ctx context.Context, funds pb.TransferFunds) (error) {
	//todo: maybe receiver
	return s.StateApi.TransferFunds(funds.Nonce, funds.Sender, funds.Receiver, funds.Amount) //todo: check for serializing bigInt
}*/

// StopService stops the grpc service.
func (s SpaceMeshGrpcService) StopService() {
	log.Debug("Stopping grpc service...")
	s.Server.Stop()
	log.Debug("grpc service stopped...")

}

// NewGrpcService create a new grpc service using config data.
func NewGrpcService(state StateAPI) *SpaceMeshGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &SpaceMeshGrpcService{Server: server, Port: uint(port), StateApi:state}
}

// StartService starts the grpc service.
func (s SpaceMeshGrpcService) StartService(status chan bool) {
	go s.startServiceInternal(status)
}

// This is a blocking method designed to be called using a go routine
func (s SpaceMeshGrpcService) startServiceInternal(status chan bool) {
	port := config.ConfigValues.GrpcServerPort
	addr := ":" + strconv.Itoa(int(port))

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen", err)
		return
	}

	pb.RegisterSpaceMeshServiceServer(s.Server, s)

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
