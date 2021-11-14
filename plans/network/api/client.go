package api

import (
	"context"
	"fmt"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc"
	"time"
)

type Client struct {
	apiCon *grpc.ClientConn

	pb.DebugServiceClient
	pb.GatewayServiceClient
	pb.GlobalStateServiceClient
	pb.MeshServiceClient
	pb.NodeServiceClient
	pb.SmesherServiceClient
	pb.TransactionServiceClient

}

// New dials the server and wraps all the api functions inside a single client
func New(server string) (*Client, error) {
	ctx, _ := context.WithTimeout(context.Background(), time.Second*10)

	apiCon, err := grpc.DialContext(ctx, server,
		grpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial server: %v", err)
	}


	c := new(Client)

	c.apiCon = apiCon

	c.DebugServiceClient = pb.NewDebugServiceClient(apiCon)
	c.GatewayServiceClient = pb.NewGatewayServiceClient(apiCon)
	c.GlobalStateServiceClient = pb.NewGlobalStateServiceClient(apiCon)
	c.MeshServiceClient = pb.NewMeshServiceClient(apiCon)
	c.NodeServiceClient = pb.NewNodeServiceClient(apiCon)
	c.SmesherServiceClient = pb.NewSmesherServiceClient(apiCon)
	c.TransactionServiceClient = pb.NewTransactionServiceClient(apiCon)

	return c, nil
}


func (c *Client) Close() error {
	return c.apiCon.Close()
}