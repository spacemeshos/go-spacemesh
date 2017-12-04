package api_tests

import (
	"github.com/UnrulyOS/go-unruly/api"
	config "github.com/UnrulyOS/go-unruly/api/config"
	"github.com/UnrulyOS/go-unruly/assert"

	"testing"

	"log"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pb "github.com/UnrulyOS/go-unruly/api/pb"

)

// write basic api test here using json-http client


func TestServersConfig(t *testing.T) {

	config.ConfigValues.GrpcServerPort = 9092
	config.ConfigValues.JsonServerPort = 9031

	if err := api.StartGrpcServer(); err != nil {
		t.Fatalf("Failed to start grpc server", err)
	}

	if err := api.StartJsonServer(); err != nil {
		t.Fatalf("Failed to start json server", err)
	}

	assert.Equal(t, api.Service.Port, config.ConfigValues.GrpcServerPort, "Expected same port")
	assert.Equal(t, api.JsonHttpServer.Port, config.ConfigValues.JsonServerPort, "Expected same port")

}

func TestGrpcService(t *testing.T) {

	const port = 9092
	const message = "Hello World"

	config.ConfigValues.GrpcServerPort = port

	if err := api.StartGrpcServer(); err != nil {
		t.FailNow()
	}

	addr := "localhost:" + string(port)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewUnrulyServiceClient(conn)

	r, err := c.Echo(context.Background(), &pb.SimpleMessage{message})
	if err != nil {
		t.Fatalf("could not greet: %v", err)
	}

	assert.Equal(t, message, r.Value, "Expected message to be echoed")

}