package tests

import (
	"fmt"
	api "github.com/UnrulyOS/go-unruly/api"
	config "github.com/UnrulyOS/go-unruly/api/config"
	"github.com/gogo/protobuf/jsonpb"
	"io/ioutil"
	"net/http"
	"strconv"
	"github.com/UnrulyOS/go-unruly/assert"
	"strings"
	"testing"
	pb "github.com/UnrulyOS/go-unruly/api/pb"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"time"
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

	// start a server
	go grpcService.StartService()

	// start a client
	addr := "localhost:" + strconv.Itoa(port)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewUnrulyServiceClient(conn)

	// call echo and validate result
	r, err := c.Echo(context.Background(), &pb.SimpleMessage{message})
	if err != nil {
		t.Fatalf("could not greet: %v", err)
	}

	assert.Equal(t, message, r.Value, "Expected message to be echoed")

	// stop the server
	grpcService.StopService()
}

func TestJsonService(t *testing.T) {

	grpcService := api.NewGrpcService()
	jsonService := api.NewJsonHttpServer()

	// start grp and json server
	go grpcService.StartService()
	go jsonService.Start()

	time.Sleep(3 * time.Second)

	const message = "hello world!"

	// generate request payload (api input params)
	reqParams := pb.SimpleMessage{Value: message}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	if err != nil {
		t.Fatalf("m.MarshalToString(%#v) failed with %v; want success", payload, err)
		return
	}

	url := fmt.Sprintf("http://localhost:%d/v1/example/echo", config.ConfigValues.JsonServerPort)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))

	if err != nil {
		t.Errorf("http.Post(%q) failed with %v; want success", url, err)
		return
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("iotuil.ReadAll(resp.Body) failed with %v; want success", err)
		return
	}

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
		t.Logf("%s", buf)
	}

	var msg pb.SimpleMessage
	if err := jsonpb.UnmarshalString(string(buf), &msg); err != nil {
		t.Errorf("jsonpb.UnmarshalString(%s, &msg) failed with %v; want success", buf, err)
		return
	}
	if got, want := msg.Value, message; got != want {
		t.Errorf("msg.Id = %q; want %q", got, want)
	}

	if value := resp.Header.Get("Content-Type"); value != "application/json" {
		t.Errorf("Content-Type was %s, wanted %s", value, "application/json")
	}
	// stop the services
	jsonService.Stop()
	grpcService.StopService()

}
