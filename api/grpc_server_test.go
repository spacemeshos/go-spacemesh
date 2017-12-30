package api

import (
	config "github.com/spacemeshos/go-spacemesh/api/config"
	pb "github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"strconv"
	"testing"
)

func TestGrpcServerConfig(t *testing.T) {

	// Arrange
	config.ConfigValues.GrpcServerPort = 9092

	// Act
	grpcService := NewGrpcService()

	// Assert
	assert.Equal(t, grpcService.Port, config.ConfigValues.GrpcServerPort, "Expected same port")
}

func TestGrpcApi(t *testing.T) {

	// Arrange
	const port = 9092
	const message = "Hello World"
	config.ConfigValues.GrpcServerPort = port

	// Act
	grpcService := NewGrpcService()

	// start a server
	grpcService.StartService()
	defer grpcService.StopService()

	// start a client
	addr := "localhost:" + strconv.Itoa(port)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())

	// Assert
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewSpaceMeshServiceClient(conn)

	// call echo and validate result
	r, err := c.Echo(context.Background(), &pb.SimpleMessage{message})
	if err != nil {
		t.Fatalf("could not greet: %v", err)
	}

	assert.Equal(t, message, r.Value, "Expected message to be echoed")
}
