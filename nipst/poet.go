package nipst

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/poet/rpc/api"
	"github.com/spacemeshos/poet/service"
	"google.golang.org/grpc"
	"time"
)

// RPCPoetClient implements PoetProvingServiceClient interface.
type RPCPoetClient struct {
	client  api.PoetClient
	CleanUp func() error
}

// A compile time check to ensure that RPCPoetClient fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*RPCPoetClient)(nil)

func (c *RPCPoetClient) submit(challenge types.Hash32) (*types.PoetRound, error) {
	req := api.SubmitRequest{Challenge: challenge[:]}
	res, err := c.client.Submit(context.Background(), &req)
	if err != nil {
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	return &types.PoetRound{Id: uint64(res.RoundId)}, nil
}

func (c *RPCPoetClient) getPoetServiceId() ([types.PoetServiceIdLength]byte, error) {
	req := api.GetInfoRequest{}
	res, err := c.client.GetInfo(context.Background(), &req)
	if err != nil {
		return [service.PoetServiceIdLength]byte{}, fmt.Errorf("rpc failure: %v", err)
	}
	var poetServiceId [service.PoetServiceIdLength]byte
	copy(poetServiceId[:], res.PoetServiceId)

	return poetServiceId, nil
}

// NewRPCPoetClient returns a new RPCPoetClient instance for the provided
// and already-connected gRPC PoetClient instance.
func NewRPCPoetClient(client api.PoetClient, cleanUp func() error) *RPCPoetClient {
	return &RPCPoetClient{
		client:  client,
		CleanUp: cleanUp,
	}
}

// NewRemoteRPCPoetClient returns a new instance of
// RPCPoetClient for the specified target.
func NewRemoteRPCPoetClient(target string, timeout time.Duration) (*RPCPoetClient, error) {
	conn, err := newClientConn(target, timeout)
	if err != nil {
		return nil, err
	}

	client := api.NewPoetClient(conn)
	cleanUp := func() error {
		return conn.Close()
	}

	return NewRPCPoetClient(client, cleanUp), nil
}

// newClientConn returns a new gRPC client
// connection to the specified target.
func newClientConn(target string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}
	defer cancel()

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rpc server: %v", err)
	}

	return conn, nil
}
