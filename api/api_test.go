package api

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	crand "crypto/rand"
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
	balances map[types.Address]*big.Int
	nonces   map[types.Address]uint64
}

type NetworkMock struct {
	broadCastErr bool
	broadcasted  []byte
}

func (s *NetworkMock) Broadcast(chanel string, payload []byte) error {
	if s.broadCastErr {
		return errors.New("error during broadcast")
	}
	s.broadcasted = payload
	return nil
}

func NewNodeAPIMock() NodeAPIMock {
	return NodeAPIMock{
		balances: make(map[types.Address]*big.Int),
		nonces:   make(map[types.Address]uint64),
	}
}

func (n NodeAPIMock) GetBalance(address types.Address) uint64 {
	return n.balances[address].Uint64()
}

func (n NodeAPIMock) GetNonce(address types.Address) uint64 {
	return n.nonces[address]
}

func (n NodeAPIMock) Exist(address types.Address) bool {
	_, ok := n.nonces[address]
	return ok
}

type TxAPIMock struct {
	mockOrigin types.Address
}

func (t *TxAPIMock) GetRewards(account types.Address) (rewards []types.Reward) {
	return
}

func (t *TxAPIMock) GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionId) {
	return
}

func (t *TxAPIMock) GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionId) {
	return
}

func (t *TxAPIMock) setMockOrigin(orig types.Address) {
	t.mockOrigin = orig
}

func (t *TxAPIMock) AddressExists(addr types.Address) bool {
	return true
}

type MinigApiMock struct {
}

func (*MinigApiMock) MiningStats() (int, string, string) {
	return 1, "123456", "/tmp"
}

func (*MinigApiMock) StartPost(address types.Address, logicalDrive string, commitmentSize uint64) error {
	return nil
}

func (*MinigApiMock) SetCoinbaseAccount(rewardAddress types.Address) {

}

type OracleMock struct {
}

func (*OracleMock) GetEligibleLayers() []types.LayerID {
	return []types.LayerID{1, 2, 3, 4}
}

type GenesisTimeMock struct {
	t time.Time
}

func (t GenesisTimeMock) GetGenesisTime() time.Time {
	return t.t
}

var (
	ap          = NodeAPIMock{}
	networkMock = NetworkMock{}
	tx          = TxAPIMock{}
	mining      = MinigApiMock{}
	oracle      = OracleMock{}
	genTime     = GenesisTimeMock{time.Now()}
)

func TestServersConfig(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2
	grpcService := NewGrpcService(&networkMock, ap, &tx, &mining, &oracle, nil, nil)
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

	grpcService := NewGrpcService(&networkMock, ap, &tx, &mining, &oracle, nil, nil)

	// start a server
	grpcService.StartService()

	// start a client
	addr := "localhost:" + strconv.Itoa(int(config.ConfigValues.GrpcServerPort))

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("did not connect. %v", err)
	}
	defer conn.Close()
	c := pb.NewSpacemeshServiceClient(conn)

	// call echo and validate result
	r, err := c.Echo(context.Background(), &pb.SimpleMessage{Value: message})
	if err != nil {
		t.Fatalf("could not greet. %v", err)
	}

	assert.Equal(t, message, r.Value, "Expected message to be echoed")

	// stop the server
	grpcService.StopService()
}

func TestJsonApi(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")

	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2
	ap := NodeAPIMock{}
	net := NetworkMock{}
	tx := TxAPIMock{}
	grpcService := NewGrpcService(&net, ap, &tx, &mining, &oracle, nil, nil)
	jsonService := NewJSONHTTPServer()

	// start grp and json server
	grpcService.StartService()

	jsonService.StartService()

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
	grpcService.StopService()
}

func TestJsonWalletApi(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")
	addrBytes := []byte{0x01}
	addr := types.BytesToAddress(addrBytes)
	if config.ConfigValues.JSONServerPort == 0 {
		config.ConfigValues.JSONServerPort = port1
		config.ConfigValues.GrpcServerPort = port2
	}
	ap := NewNodeAPIMock()
	net := NetworkMock{broadcasted: []byte{0x00}}
	ap.nonces[addr] = 10
	ap.balances[addr] = big.NewInt(100)
	txApi := TxAPIMock{}
	grpcService := NewGrpcService(&net, ap, &txApi, &mining, &oracle, &genTime, nil)
	jsonService := NewJSONHTTPServer()

	// start grp and json server
	grpcService.StartService()
	jsonService.StartService()

	const message = "10"
	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.AccountId{Address: util.Bytes2Hex(addrBytes)}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	assert.NoError(t, err, "failed to marshal to string")

	// Without this running this on Travis CI might generate a connection refused error
	// because the server may not be ready to accept connections just yet.
	time.Sleep(3 * time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/nonce", config.ConfigValues.JSONServerPort)
	resp, err := http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")
	resp.Body.Close()

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

	buf, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")
	resp.Body.Close()
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	err = jsonpb.UnmarshalString(string(buf), &msg)
	assert.NoError(t, err)

	gotV, wantV := msg.Value, "100"
	assert.Equal(t, wantV, gotV)

	value = resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)

	sPub, key, _ := ed25519.GenerateKey(crand.Reader)
	sAddr := state.PublicKeyToAccountAddress(sPub)
	txApi.setMockOrigin(sAddr)
	rec := types.BytesToAddress([]byte{0xde, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa})
	signer, err := signing.NewEdSignerFromBuffer(key)
	assert.NoError(t, err)
	tx, err := mesh.NewSignedTx(1111, rec, 1234, 11, 321, signer)
	assert.NoError(t, err)
	txBytes, err := types.InterfaceToBytes(tx)
	assert.NoError(t, err)
	txToSend := pb.SignedTransaction{Tx: txBytes}

	var buf2 bytes.Buffer
	err = m.Marshal(&buf2, &txToSend)
	assert.NoError(t, err, "failed to marshal pb")

	url = fmt.Sprintf("http://127.0.0.1:%d/v1/submittransaction", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(buf2.String())) //string(payload2))) //todo: we currently accept all kinds of payloads
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")
	resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	ret := pb.TxConfirmation{}
	err = jsonpb.UnmarshalString(string(buf), &ret)
	assert.NoError(t, err)

	gotV, wantV = ret.Value, "ok"
	assert.Equal(t, wantV, gotV)

	id := tx.Id()
	hexIdStr := hex.EncodeToString(id[:])

	assert.Equal(t, hexIdStr, ret.Id)
	val, err := types.InterfaceToBytes(tx)
	assert.NoError(t, err)
	assert.Equal(t, val, net.broadcasted)

	value = resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)

	reqPost := pb.InitPost{Coinbase: "0x1234", LogicalDrive: "/tmp/aaa", CommitmentSize: 2048}
	payload, err = m.MarshalToString(&reqPost)
	assert.NoError(t, err, "failed to marshal to string")

	url = fmt.Sprintf("http://127.0.0.1:%d/v1/startmining", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	assert.NoError(t, err, "failed to read response body")

	got, want = resp.StatusCode, http.StatusOK
	assert.Equal(t, want, got)

	// test get statistics about init progress
	url = fmt.Sprintf("http://127.0.0.1:%d/v1/stats", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, nil)
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	assert.NoError(t, err, "failed to read response body")

	got, want = resp.StatusCode, http.StatusOK
	assert.Equal(t, want, got)

	var stats pb.MiningStats
	err = jsonpb.UnmarshalString(string(buf), &stats)
	assert.NoError(t, err)

	assert.Equal(t, int32(1), stats.Status)
	assert.Equal(t, "/tmp", stats.DataDir)
	assert.Equal(t, "123456", stats.Coinbase)

	// test get genesisTime
	url = fmt.Sprintf("http://127.0.0.1:%d/v1/genesis", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, nil)
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	assert.NoError(t, err, "failed to read response body")

	got, want = resp.StatusCode, http.StatusOK
	assert.Equal(t, want, got)

	err = jsonpb.UnmarshalString(string(buf), &msg)
	assert.NoError(t, err)

	assert.Equal(t, genTime.t.String(), msg.Value)

	// test get rewards per account
	reqParams = pb.AccountId{Address: util.Bytes2Hex(addrBytes)}
	payload, err = m.MarshalToString(&reqParams)
	url = fmt.Sprintf("http://127.0.0.1:%d/v1/accountrewards", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	assert.NoError(t, err, "failed to read response body")

	got, want = resp.StatusCode, http.StatusOK
	assert.Equal(t, want, got)

	var rewards pb.AccountRewards
	err = jsonpb.UnmarshalString(string(buf), &rewards)
	assert.NoError(t, err)

	// test get txs per account
	reqParam := pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addrBytes)}, StartLayer: 1, EndLayer: 2}
	payload, err = m.MarshalToString(&reqParam)
	url = fmt.Sprintf("http://127.0.0.1:%d/v1/accounttxs", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	assert.NoError(t, err, "failed to read response body")

	got, want = resp.StatusCode, http.StatusOK
	assert.Equal(t, want, got)

	var accounts pb.AccountTxs
	err = jsonpb.UnmarshalString(string(buf), &accounts)
	assert.NoError(t, err)

	// test get txs per account with wrong layer error
	reqParam = pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addrBytes)}, StartLayer: 2, EndLayer: 1}
	payload, err = m.MarshalToString(&reqParam)
	url = fmt.Sprintf("http://127.0.0.1:%d/v1/accounttxs", config.ConfigValues.JSONServerPort)
	resp, err = http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	buf, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	assert.NoError(t, err, "failed to read response body")
	if got, want := resp.StatusCode, http.StatusInternalServerError; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
	}

	// stop the services
	jsonService.StopService()
	grpcService.StopService()
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
	tx := TxAPIMock{}
	grpcService := NewGrpcService(&net, ap, &tx, &mining, &oracle, nil, nil)
	jsonService := NewJSONHTTPServer()

	// start grp and json server
	grpcService.StartService()

	jsonService.StartService()

	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.AccountId{Address: util.Bytes2Hex(addrBytes)}
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
	grpcService.StopService()
}

func TestSpaceMeshGrpcService_Broadcast(t *testing.T) {

	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")
	Data := "l33t"
	if config.ConfigValues.JSONServerPort == 0 {
		config.ConfigValues.JSONServerPort = port1
		config.ConfigValues.GrpcServerPort = port2
	}
	ap := NewNodeAPIMock()
	net := NetworkMock{broadcasted: []byte{0x00}}
	tx := TxAPIMock{}
	grpcService := NewGrpcService(&net, ap, &tx, &mining, &oracle, nil, nil)
	jsonService := NewJSONHTTPServer()

	// start grp and json server
	grpcService.StartService()

	jsonService.StartService()

	const message = "ok"
	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.BroadcastMessage{Data: Data}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	assert.NoError(t, err, "failed to marshal to string")

	// Without this running this on Travis CI might generate a connection refused error
	// because the server may not be ready to accept connections just yet.
	time.Sleep(3 * time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/broadcast", config.ConfigValues.JSONServerPort)
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

	assert.Equal(t, Data, string(net.broadcasted))

	// stop the services
	jsonService.StopService()
	grpcService.StopService()
}

func TestSpaceMeshGrpcService_BroadcastErrors(t *testing.T) {
	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Should be able to establish a connection on a port")
	Data := "l337"
	if config.ConfigValues.JSONServerPort == 0 {
		config.ConfigValues.JSONServerPort = port1
		config.ConfigValues.GrpcServerPort = port2
	}
	ap := NewNodeAPIMock()
	net := NetworkMock{broadcasted: []byte{0x00}}
	net.broadCastErr = true
	tx := TxAPIMock{}
	grpcService := NewGrpcService(&net, ap, &tx, &mining, &oracle, nil, nil)
	jsonService := NewJSONHTTPServer()

	// start grp and json server
	grpcService.StartService()
	jsonService.StartService()

	const contentType = "application/json"

	// generate request payload (api input params)
	reqParams := pb.BroadcastMessage{Data: Data}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	assert.NoError(t, err, "failed to marshal to string")

	// Without this running this on Travis CI might generate a connection refused error
	// because the server may not be ready to accept connections just yet.
	time.Sleep(3 * time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/broadcast", config.ConfigValues.JSONServerPort)
	resp, err := http.Post(url, contentType, strings.NewReader(payload))
	assert.NoError(t, err, "failed to http post to api endpoint")

	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err, "failed to read response body")

	got, want := resp.StatusCode, http.StatusInternalServerError //todo: should we change it to err 400 somehow?
	assert.Equal(t, want, got)

	value := resp.Header.Get("Content-Type")
	assert.Equal(t, value, contentType)

	// stop the services
	jsonService.StopService()
	grpcService.StopService()
}

type mockSrv struct {
	c      chan service.GossipMessage
	called bool
}

func (m *mockSrv) RegisterGossipProtocol(string) chan service.GossipMessage {
	m.called = true
	return m.c
}

type mockMsg struct {
	sender p2pcrypto.PublicKey
	msg    []byte
	c      chan service.MessageValidation
}

func (m *mockMsg) Bytes() []byte {
	return m.msg
}

func (m *mockMsg) ValidationCompletedChan() chan service.MessageValidation {
	return m.c
}

func (m *mockMsg) Sender() p2pcrypto.PublicKey {
	return m.sender
}

func (m *mockMsg) ReportValidation(protocol string) {
	m.c <- service.NewMessageValidation(m.sender, m.msg, protocol)
}

func TestApproveAPIGossipMessages(t *testing.T) {
	m := &mockSrv{c: make(chan service.GossipMessage, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	ApproveAPIGossipMessages(ctx, m)
	require.True(t, m.called)
	somekey := p2pcrypto.NewRandomPubkey()
	msg := &mockMsg{somekey, []byte("TEST"), make(chan service.MessageValidation, 1)}
	m.c <- msg
	res := <-msg.ValidationCompletedChan()
	require.NotNil(t, res)
	require.Equal(t, res.Sender(), somekey)
	require.Equal(t, res.Message(), []byte("TEST"))
	require.Equal(t, res.Protocol(), APIGossipProtocol)
	cancel()
}
