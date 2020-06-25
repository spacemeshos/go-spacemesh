package grpcserver

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/miner"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// Better a small code duplication than a small dependency

type NodeAPIMock struct {
	balances map[types.Address]*big.Int
	nonces   map[types.Address]uint64
}

type NetworkMock struct {
	lock         sync.Mutex
	broadCastErr bool
	broadcasted  []byte
}

func (s *NetworkMock) SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey) {
	return make(chan p2pcrypto.PublicKey), make(chan p2pcrypto.PublicKey)
}

func (s *NetworkMock) Broadcast(_ string, payload []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.broadCastErr {
		return errors.New("error during broadcast")
	}
	s.broadcasted = payload
	return nil
}

func (s *NetworkMock) GetBroadcast() []byte {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.broadcasted
}

func (s *NetworkMock) SetErr(err bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.broadCastErr = err
}

func (s *NetworkMock) GetErr() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.broadCastErr
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
	mockOrigin   types.Address
	returnTx     map[types.TransactionID]*types.Transaction
	layerApplied map[types.TransactionID]*types.LayerID
	err          error
}

func (t *TxAPIMock) GetStateRoot() types.Hash32 {
	var hash types.Hash32
	hash.SetBytes([]byte("00000"))
	return hash
}

func (t *TxAPIMock) ValidateNonceAndBalance(*types.Transaction) error {
	return t.err
}

func (t *TxAPIMock) GetProjection(_ types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	return prevNonce, prevBalance, nil
}

func (t *TxAPIMock) LatestLayerInState() types.LayerID {
	return ValidatedLayerID
}

func (t *TxAPIMock) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	return t.layerApplied[txID]
}

func (t *TxAPIMock) GetTransaction(id types.TransactionID) (*types.Transaction, error) {
	return t.returnTx[id], nil
}

func (t *TxAPIMock) LatestLayer() types.LayerID {
	return 10
}

func (t *TxAPIMock) GetRewards(types.Address) (rewards []types.Reward, err error) {
	return
}

func (t *TxAPIMock) GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionID) {
	if l != TxReturnLayer {
		return nil
	}
	for _, tx := range t.returnTx {
		if tx.Recipient.String() == account.String() {
			txs = append(txs, tx.ID())
		}
	}
	return
}

func (t *TxAPIMock) GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionID) {
	if l != TxReturnLayer {
		return nil
	}
	for _, tx := range t.returnTx {
		if tx.Origin().String() == account.String() {
			txs = append(txs, tx.ID())
		}
	}
	return
}

func (t *TxAPIMock) setMockOrigin(orig types.Address) {
	t.mockOrigin = orig
}

func (t *TxAPIMock) AddressExists(types.Address) bool {
	return true
}

// MiningAPIMock is a mock for mining API
type MiningAPIMock struct{}

const (
	miningStatus   = 123
	remainingBytes = 321
)

func (*MiningAPIMock) MiningStats() (int, uint64, string, string) {
	return miningStatus, remainingBytes, "123456", "/tmp"
}

func (*MiningAPIMock) StartPost(types.Address, string, uint64) error {
	return nil
}

func (*MiningAPIMock) SetCoinbaseAccount(types.Address) {}

type OracleMock struct{}

func (*OracleMock) GetEligibleLayers() []types.LayerID {
	return []types.LayerID{1, 2, 3, 4}
}

type GenesisTimeMock struct {
	t time.Time
}

func (t GenesisTimeMock) GetCurrentLayer() types.LayerID {
	return 1
}

func (t GenesisTimeMock) GetGenesisTime() time.Time {
	return t.t
}

type PostMock struct {
}

func (PostMock) Reset() error {
	return nil
}

const (
	genTimeUnix      = 1000000
	layerDuration    = 10
	ValidatedLayerID = 8
	TxReturnLayer    = 1
)

var (
	ap          = NewNodeAPIMock()
	networkMock = NetworkMock{}
	mining      = MiningAPIMock{}
	oracle      = OracleMock{}
	genTime     = GenesisTimeMock{time.Unix(genTimeUnix, 0)}
	txMempool   = miner.NewTxMemPool()
	txAPI       = &TxAPIMock{
		returnTx:     make(map[types.TransactionID]*types.Transaction),
		layerApplied: make(map[types.TransactionID]*types.LayerID),
	}
)

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

var cfg = config.DefaultConfig()

type SyncerMock struct{}

func (SyncerMock) IsSynced() bool { return false }
func (SyncerMock) Start()         {}

func launchServer(t *testing.T, services ...ServiceServer) func() {
	networkMock.Broadcast("", []byte{0x00})
	jsonService := NewJSONHTTPServer(cfg.NewJSONServerPort, cfg.NewGrpcServerPort)
	// start gRPC and json servers
	for _, s := range services {
		require.NoError(t, StartService(s))
	}
	jsonService.StartService()

	time.Sleep(3 * time.Second) // wait for server to be ready (critical on Travis)

	return func() {
		require.NoError(t, jsonService.Close())

		// We only actually need to close one of these, since closing one
		// closes them all, but mimic in-app behavior here. It should not
		// hurt to close them all.
		for _, s := range services {
			require.NoError(t, s.Close())
		}
	}
}

func callEndpoint(t *testing.T, endpoint, payload string) (string, int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.NewJSONServerPort, endpoint)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return string(buf), resp.StatusCode
}

func TestNewServersConfig(t *testing.T) {
	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	require.NoError(t, err, "Should be able to establish a connection on a port")

	grpcService := NewNodeService(
		port1, &networkMock, txAPI, nil, nil)

	jsonService := NewJSONHTTPServer(port2, port1)
	require.Equal(t, port2, jsonService.Port, "Expected same port")
	require.Equal(t, port1, jsonService.GrpcPort, "Expected same port")
	require.Equal(t, port1, grpcService.Port(), "Expected same port")
}

func TestNodeService(t *testing.T) {
	grpcService := NewNodeService(cfg.NewGrpcServerPort, &networkMock, txAPI, &genTime, &SyncerMock{})
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	const message = "Hello World"

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewNodeServiceClient(conn)

	// call echo and validate result
	response, err := c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	require.NoError(t, err)
	require.Equal(t, message, response.Msg.Value)
}

func TestMeshService(t *testing.T) {
	grpcService := NewMeshService(cfg.NewGrpcServerPort, &networkMock, txAPI, &genTime, &SyncerMock{})
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewMeshServiceClient(conn)

	// call echo and validate result
	response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), response.Unixtime.Value)
}

func TestMultiService(t *testing.T) {
	svc1 := NewNodeService(cfg.NewGrpcServerPort, &networkMock, txAPI, &genTime, &SyncerMock{})
	svc2 := NewMeshService(cfg.NewGrpcServerPort, &networkMock, txAPI, &genTime, &SyncerMock{})
	shutDown := launchServer(t, svc1, svc2)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewMeshServiceClient(conn)

	// call echo and validate result
	response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), response.Unixtime.Value)
}

func TestJsonApi(t *testing.T) {
	grpcService := NewNodeService(cfg.NewGrpcServerPort, &networkMock, txAPI, &genTime, &SyncerMock{})
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	const message = "hello world!"

	// generate request payload (api input params)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})

	respBody, respStatus := callEndpoint(t, "v1/node/echo", payload)
	require.Equal(t, http.StatusOK, respStatus)
	var msg pb.EchoResponse
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}
