package tests

import (
	api "github.com/UnrulyOS/go-unruly/api"
	config "github.com/UnrulyOS/go-unruly/api/config"
	//"time"

	//"google.golang.org/grpc/grpclb/grpc_lb_v1/service"

	"strconv"


	"github.com/UnrulyOS/go-unruly/assert"

	"testing"

	pb "github.com/UnrulyOS/go-unruly/api/pb"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// write basic api test here using json-http client

func TestServersConfig(t *testing.T) {

	config.ConfigValues.GrpcServerPort = 9092
	config.ConfigValues.JsonServerPort = 9031

	grpcService := api.NewGrpcService()
	jsonService := api.NewJsonHttpServer()

	assert.Equal(t, grpcService.Port, config.ConfigValues.GrpcServerPort, "Expected same port")
	assert.Equal(t, jsonService.Port, config.ConfigValues.JsonServerPort, "Expected same port")

}


func TestGrpcService(t *testing.T) {

	const port = 9092
	const message = "Hello World"
	config.ConfigValues.GrpcServerPort = port

	grpcService := api.NewGrpcService()

	// start the server
	go grpcService.StartService()

	// give some time to start
	// time.Sleep(1 * time.Second)

	// start client

	addr := "localhost:" + strconv.Itoa(port)

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

	grpcService.StopService()

}
