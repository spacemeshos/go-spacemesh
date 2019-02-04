package api

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/address"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// Better a small code duplication than a small dependency


type NodeAPIMock struct {
	balances map[address.Address]*big.Int
	nonces map[address.Address]uint64
}

type NetworkMock struct {
	broadcasted []byte
}

func (s *NetworkMock) Broadcast(chanel string, payload []byte) error{
	s.broadcasted = payload
	return nil
}

func NewNodeAPIMock() NodeAPIMock{
	return NodeAPIMock{
		balances : make(map[address.Address]*big.Int),
		nonces : make(map[address.Address]uint64),
	}
}

func ( n NodeAPIMock) GetBalance(address address.Address) *big.Int {
	return n.balances[address]
}

func (n NodeAPIMock) GetNonce(address address.Address) uint64 {
	return n.nonces[address]
}

func (n NodeAPIMock) Exist(address address.Address) bool{
	_, ok := n.nonces[address]
	return ok
}

func TestServersConfig(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2
	ap := NodeAPIMock{}
	net := NetworkMock{}
	grpcService := NewGrpcService(&net, ap)
	jsonService := NewJSONHTTPServer()

	assert.Equal(t, grpcService.Port, uint(config.ConfigValues.GrpcServerPort), "Expected same port")
	assert.Equal(t, jsonService.Port, uint(config.ConfigValues.JSONServerPort), "Expected same port")
}

func TestGrpcApi(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2

	const message = "Hello World"
	ap := NodeAPIMock{}
	net := NetworkMock{}

	grpcService := NewGrpcService(&net, ap)
	grpcStatus := make(chan bool, 2)

	// start a server
	grpcService.StartService(grpcStatus)
	<-grpcStatus

	// start a client
	addr := "localhost:" + strconv.Itoa(int(config.ConfigValues.GrpcServerPort))

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("did not connect. %v", err)
	}
	defer conn.Close()
	c := pb.NewSpaceMeshServiceClient(conn)

	// call echo and validate result
	r, err := c.Echo(context.Background(), &pb.SimpleMessage{Value: message})
	if err != nil {
		t.Fatalf("could not greet. %v", err)
	}

	assert.Equal(t, message, r.Value, "Expected message to be echoed")

	// stop the server
	grpcService.StopService()
	<-grpcStatus
}

func TestJsonApi(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2
	ap := NodeAPIMock{}
	net := NetworkMock{}
	grpcService := NewGrpcService(&net, ap)
	jsonService := NewJSONHTTPServer()

	jsonStatus := make(chan bool, 2)
	grpcStatus := make(chan bool, 2)

	// start grp and json server
	grpcService.StartService(grpcStatus)
	<-grpcStatus

	jsonService.StartService(jsonStatus)
	<-jsonStatus

	const message = "hello world!"
	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.SimpleMessage{Value: message}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	assert.NoError(t, err, "failed to marshal to string")

	// Without this running this on Travis CI might generate a connection refused error
	// because the server may not be ready to accept connections just yet.
	time.Sleep(3 * time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/example/echo", config.ConfigValues.JSONServerPort)
	resp, err := http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	var msg pb.SimpleMessage
	if err := jsonpb.UnmarshalString(string(buf), &msg); err != nil {
		t.Errorf("jsonpb.UnmarshalString(%s, &msg) failed with %v; want success", buf, err)
		return
	}

	if got, want := msg.Value, message; got != want {
		t.Errorf("msg.Value = %q; want %q", got, want)
	}

	if value := resp.Header.Get("Content-Type"); value != contentType {
		t.Errorf("Content-Type was %s, wanted %s", value, contentType)
	}

	// stop the services
	jsonService.StopService()
	<-jsonStatus
	grpcService.StopService()
	<-grpcStatus
}



func TestJsonWalletApi(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")
	addrBytes := []byte{0x01}
	addr := address.BytesToAddress(addrBytes)
	if config.ConfigValues.JSONServerPort == 0 {
		config.ConfigValues.JSONServerPort = port1
		config.ConfigValues.GrpcServerPort = port2
	}
	ap := NewNodeAPIMock()
	net := NetworkMock{ broadcasted:[]byte{0x00}}
	ap.nonces[addr] = 10
	ap.balances[addr] = big.NewInt(100)
	grpcService := NewGrpcService(&net, ap)
	jsonService := NewJSONHTTPServer()

	jsonStatus := make(chan bool, 2)
	grpcStatus := make(chan bool, 2)

	// start grp and json server
	grpcService.StartService(grpcStatus)
	<-grpcStatus

	jsonService.StartService(jsonStatus)
	<-jsonStatus

	const message = "10"
	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.AccountId{Address: addrBytes}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	assert.NoError(t, err, "failed to marshal to string")

	// Without this running this on Travis CI might generate a connection refused error
	// because the server may not be ready to accept connections just yet.
	time.Sleep(3 * time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/nonce", config.ConfigValues.JSONServerPort)
	resp, err := http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")

	got, want := resp.StatusCode, http.StatusOK
	assert.Equal(t, want, got)

	var msg pb.SimpleMessage
	err = jsonpb.UnmarshalString(string(buf), &msg)
	assert.NoError(t, err)

	gotVal, wantVal := msg.Value, message
	assert.Equal(t, wantVal, gotVal)

	value := resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)

	url = fmt.Sprintf("http://127.0.0.1:%d/v1/balance", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()
	buf, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	err = jsonpb.UnmarshalString(string(buf), &msg)
	assert.NoError(t, err)

	gotV, wantV := msg.Value, "100"
	assert.Equal(t, wantV, gotV)

	value = resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)


	txParams := pb.SignedTransaction{TxData:[]byte{0x00, 0x01,0x02,0x03}}
	payload, err = m.MarshalToString(&txParams)
	url = fmt.Sprintf("http://127.0.0.1:%d/v1/submittransaction", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload)) //todo: we currently accept all kinds of payloads
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()
	buf, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	err = jsonpb.UnmarshalString(string(buf), &msg)
	assert.NoError(t, err)

	gotV, wantV = msg.Value, "ok"
	assert.Equal(t, wantV, gotV)

	_, err = proto.Marshal(&txParams)
	assert.NoError(t, err)

	assert.Equal(t, txParams.TxData, net.broadcasted)

	value = resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)

	// stop the services
	jsonService.StopService()
	<-jsonStatus
	grpcService.StopService()
	<-grpcStatus
}

func TestJsonWalletApi_Errors(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")
	addrBytes := []byte{0x01}
	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2
	ap := NewNodeAPIMock()
	net := NetworkMock{}


	grpcService := NewGrpcService(&net, ap)
	jsonService := NewJSONHTTPServer()

	jsonStatus := make(chan bool, 2)
	grpcStatus := make(chan bool, 2)

	// start grp and json server
	grpcService.StartService(grpcStatus)
	<-grpcStatus

	jsonService.StartService(jsonStatus)
	<-jsonStatus

	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.AccountId{Address: addrBytes}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	assert.NoError(t, err, "failed to marshal to string")

	// Without this running this on Travis CI might generate a connection refused error
	// because the server may not be ready to accept connections just yet.
	time.Sleep(3 * time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/nonce", config.ConfigValues.JSONServerPort)
	resp, err := http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")

	got, want := resp.StatusCode, http.StatusInternalServerError //todo: should we change it to err 400 somehow?
	assert.Equal(t, want, got)


	value := resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)

	url = fmt.Sprintf("http://127.0.0.1:%d/v1/balance", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusInternalServerError; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	value = resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)


	// stop the services
	jsonService.StopService()
	<-jsonStatus
	grpcService.StopService()
	<-grpcStatus
}