package activation

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/poet/rpc/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"time"
)

// RPCPoetClient implements PoetProvingServiceClient interface.
type RPCPoetClient struct {
	client   api.PoetClient
	Teardown func(cleanup bool) error
}

// A compile time check to ensure that RPCPoetClient fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*RPCPoetClient)(nil)

func (c *RPCPoetClient) Start(nodeAddress string) error {
	req := api.StartRequest{NodeAddress: nodeAddress}
	_, err := c.client.Start(context.Background(), &req)
	if err != nil {
		return fmt.Errorf("rpc failure: %v", err)
	}

	return nil
}

func (c *RPCPoetClient) submit(challenge types.Hash32) (*types.PoetRound, error) {
	req := api.SubmitRequest{Challenge: challenge[:]}
	res, err := c.client.Submit(context.Background(), &req)
	if err != nil {
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	return &types.PoetRound{Id: res.RoundId}, nil
}

func (c *RPCPoetClient) getPoetServiceId() ([]byte, error) {
	req := api.GetInfoRequest{}
	res, err := c.client.GetInfo(context.Background(), &req)
	if err != nil {
		return []byte{}, fmt.Errorf("rpc failure: %v", err)
	}
	var poetServiceId []byte
	copy(poetServiceId[:], res.ServicePubKey)

	return poetServiceId, nil
}

// NewRPCPoetClient returns a new RPCPoetClient instance for the provided
// and already-connected gRPC PoetClient instance.
func NewRPCPoetClient(client api.PoetClient, cleanUp func(cleanup bool) error) *RPCPoetClient {
	return &RPCPoetClient{
		client:   client,
		Teardown: cleanUp,
	}
}

// NewRemoteRPCPoetClient returns a new instance of
// RPCPoetClient for the specified target.
func NewRemoteRPCPoetClient(target string, ctx context.Context) (*RPCPoetClient, error) {
	conn, err := newClientConn(target, ctx)
	if err != nil {
		return nil, err
	}

	client := api.NewPoetClient(conn)
	cleanUp := func(cleanup bool) error {
		return conn.Close()
	}

	return NewRPCPoetClient(client, cleanUp), nil
}

// newClientConn returns a new gRPC client
// connection to the specified target.
func newClientConn(target string, ctx context.Context) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
		// XXX: this is done to prevent routers from cleaning up our connections (e.g aws load balances..)
		// TODO: these parameters work for now but we might need to revisit or add them as configuration
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			time.Minute,
			time.Minute * 3,
			true,
		})}

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rpc server: %v", err)
	}

	return conn, nil
}
