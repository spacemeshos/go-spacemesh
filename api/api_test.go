package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	config2 "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/stretchr/testify/require"

	crand "crypto/rand"

	"github.com/golang/protobuf/jsonpb"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
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

type res struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
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

func (t *TxAPIMock) GetATXs([]types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID) {
	return nil, nil
}

func (t *TxAPIMock) GetTransactions([]types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
	return nil, nil
}

func (t *TxAPIMock) GetLayer(types.LayerID) (*types.Layer, error) {
	return nil, nil
}

func (t *TxAPIMock) ProcessedLayer() types.LayerID {
	panic("implement me")
}

func (t *TxAPIMock) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (t *TxAPIMock) GetBalance(addr types.Address) uint64 {
	panic("implement me")
}
func (t *TxAPIMock) GetNonce(addr types.Address) uint64 {
	panic("implement me")
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

func (*MiningAPIMock) GetSmesherID() types.NodeID {
	panic("implement me")
}

func (*MiningAPIMock) Stop() {
	panic("implement me")
}

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
	txMempool   = state.NewTxMemPool()
	txAPI       = &TxAPIMock{
		returnTx:     make(map[types.TransactionID]*types.Transaction),
		layerApplied: make(map[types.TransactionID]*types.LayerID),
	}
)

func TestServersConfig(t *testing.T) {
	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	require.NoError(t, err, "Should be able to establish a connection on a port")

	grpcService := NewGrpcService(port1, &networkMock, ap, txAPI, nil, &mining, &oracle, nil, PostMock{}, 0, nil, nil, nil)
	require.Equal(t, grpcService.Port, uint(port1), "Expected same port")

	jsonService := NewJSONHTTPServer(port2, port1)
	require.Equal(t, jsonService.Port, uint(port2), "Expected same port")
	require.Equal(t, jsonService.GrpcPort, uint(port1), "Expected same port")
}

func TestGrpcApi(t *testing.T) {
	shutDown := launchServer(t)

	const message = "Hello World"

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.GrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewSpacemeshServiceClient(conn)

	// call echo and validate result
	response, err := c.Echo(context.Background(), &pb.SimpleMessage{Value: message})
	require.NoError(t, err)

	require.Equal(t, message, response.Value)

	// stop the server
	shutDown()
}

func TestJsonApi(t *testing.T) {
	shutDown := launchServer(t)

	const message = "hello world!"

	// generate request payload (api input params)
	payload := marshalProto(t, &pb.SimpleMessage{Value: message})

	respBody, respStatus := callEndpoint(t, "v1/example/echo", payload)
	require.Equal(t, http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, message)

	// stop the services
	shutDown()
}

func TestBroadcastPoet(t *testing.T) {
	r := require.New(t)
	shutDown := launchServer(t)

	payload := "{\"data\":[1,2,3]}"
	respBody, respStatus := callEndpoint(t, "v1/broadcastpoet", payload)
	r.Equal(http.StatusOK, respStatus, http.StatusText(respStatus))
	assertSimpleMessage(t, respBody, "ok")

	r.Equal([]byte{1, 2, 3}, networkMock.GetBroadcast())

	shutDown()
}

func TestJsonWalletApi(t *testing.T) {
	r := require.New(t)
	shutDown := launchServer(t)

	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	ap.nonces[addr] = 10
	ap.balances[addr] = big.NewInt(100)

	// generate request payload (api input params)
	payload := marshalProto(t, &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())})

	respBody, respStatus := callEndpoint(t, "v1/nonce", payload)
	r.Equal(http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, "10")

	respBody, respStatus = callEndpoint(t, "v1/balance", payload)
	r.Equal(http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, "100")

	// Test submit transaction
	submitTx(t, genTx(t))

	txToSend := pb.SignedTransaction{Tx: asBytes(t, genTx(t))}
	msg := "nonce or balance validation failed"
	expectedResponse := "{\"error\":\"" + msg + "\",\"message\":\"" + msg + "\",\"code\":2}"
	expected := res{}
	res1 := res{}
	json.Unmarshal([]byte(expectedResponse), &expected)
	txAPI.err = fmt.Errorf(msg)
	respBody, respStatus = callEndpoint(t, "v1/submittransaction", marshalProto(t, &txToSend))
	json.Unmarshal([]byte(respBody), &res1)
	txAPI.err = nil
	r.Equal(http.StatusInternalServerError, respStatus, http.StatusText(respStatus))
	r.Equal(expected, res1)

	// test start mining
	initPostRequest := pb.InitPost{Coinbase: "0x1234", LogicalDrive: "/tmp/aaa", CommitmentSize: 2048}
	respBody, respStatus = callEndpoint(t, "v1/startmining", marshalProto(t, &initPostRequest))
	r.Equal(http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, "ok")

	// test get statistics about init progress
	respBody, respStatus = callEndpoint(t, "v1/stats", "")
	r.Equal(http.StatusOK, respStatus)

	var stats pb.MiningStats
	r.NoError(jsonpb.UnmarshalString(respBody, &stats))

	r.Equal(int32(miningStatus), stats.Status)
	r.Equal("/tmp", stats.DataDir)
	r.Equal("123456", stats.Coinbase)
	r.Equal(uint64(remainingBytes), stats.RemainingBytes)

	// test get node status
	respBody, respStatus = callEndpoint(t, "v1/nodestatus", "")
	r.Equal(http.StatusOK, respStatus)

	var nodeStatus pb.NodeStatus
	r.NoError(jsonpb.UnmarshalString(respBody, &nodeStatus))

	r.Zero(nodeStatus.Peers)
	r.Equal(uint64(5), nodeStatus.MinPeers)
	r.Equal(uint64(105), nodeStatus.MaxPeers)
	r.False(nodeStatus.Synced)
	r.Equal(uint64(10), nodeStatus.SyncedLayer)
	r.Equal(uint64(1), nodeStatus.CurrentLayer)
	r.Equal(uint64(8), nodeStatus.VerifiedLayer)

	// test get genesisTime
	respBody, respStatus = callEndpoint(t, "v1/genesis", "")
	r.Equal(http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, genTime.t.Format(time.RFC3339))

	// test get rewards per account
	payload = marshalProto(t, &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())})
	respBody, respStatus = callEndpoint(t, "v1/accountrewards", payload)
	r.Equal(http.StatusOK, respStatus)

	var rewards pb.AccountRewards
	r.NoError(jsonpb.UnmarshalString(respBody, &rewards))
	r.Empty(rewards.Rewards) // TODO: Test with actual data returned

	// test get txs per account:

	// add incoming tx to mempool
	mempoolTxIn, err := types.NewSignedTx(1337, addr, 420, 3, 42, signing.NewEdSigner())
	r.NoError(err)
	txMempool.Put(mempoolTxIn.ID(), mempoolTxIn)

	// add outgoing tx to mempool
	mempoolTxOut, err := types.NewSignedTx(1337, types.BytesToAddress([]byte{1}), 420, 3, 42, signer)
	r.NoError(err)
	txMempool.Put(mempoolTxOut.ID(), mempoolTxOut)

	// add incoming tx to mesh
	meshTxIn, err := types.NewSignedTx(1337, addr, 420, 3, 42, signing.NewEdSigner())
	r.NoError(err)
	txAPI.returnTx[meshTxIn.ID()] = meshTxIn

	// add outgoing tx to mesh
	meshTxOut, err := types.NewSignedTx(1337, types.BytesToAddress([]byte{1}), 420, 3, 42, signer)
	r.NoError(err)
	txAPI.returnTx[meshTxOut.ID()] = meshTxOut

	// test with start layer that gets the mesh txs
	payload = marshalProto(t, &pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())}, StartLayer: TxReturnLayer})
	respBody, respStatus = callEndpoint(t, "v1/accounttxs", payload)
	r.Equal(http.StatusOK, respStatus)

	var accounts pb.AccountTxs
	r.NoError(jsonpb.UnmarshalString(respBody, &accounts))
	r.Equal(uint64(ValidatedLayerID), accounts.ValidatedLayer)
	r.ElementsMatch([]string{
		mempoolTxIn.ID().String(),
		mempoolTxOut.ID().String(),
		meshTxIn.ID().String(),
		meshTxOut.ID().String(),
	}, accounts.Txs)

	// test with start layer that doesn't get the mesh txs (mempool txs return anyway)
	payload = marshalProto(t, &pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())}, StartLayer: TxReturnLayer + 1})
	respBody, respStatus = callEndpoint(t, "v1/accounttxs", payload)
	r.Equal(http.StatusOK, respStatus)

	r.NoError(jsonpb.UnmarshalString(respBody, &accounts))
	r.Equal(uint64(ValidatedLayerID), accounts.ValidatedLayer)
	r.ElementsMatch([]string{
		mempoolTxIn.ID().String(),
		mempoolTxOut.ID().String(),
	}, accounts.Txs)

	// test get txs per account with wrong layer error
	payload = marshalProto(t, &pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())}, StartLayer: 11})
	respBody, respStatus = callEndpoint(t, "v1/accounttxs", payload)
	r.Equal(http.StatusInternalServerError, respStatus)
	const ErrInvalidStartLayer = "{\"error\":\"invalid start layer\",\"message\":\"invalid start layer\",\"code\":2}"
	expected2 := res{}
	res2 := res{}
	json.Unmarshal([]byte(ErrInvalidStartLayer), &expected2)
	json.Unmarshal([]byte(respBody), &res2)
	r.Equal(expected2, res2)

	// test call reset post
	respBody, respStatus = callEndpoint(t, "v1/resetpost", "")
	r.Equal(http.StatusOK, respStatus)

	// test get getStateRoot
	respBody, respStatus = callEndpoint(t, "v1/stateroot", "")
	r.Equal(http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, "0x0000000000000000000000000000000000000000000000000000003030303030")

	// stop the services
	shutDown()
}

func asBytes(t *testing.T, tx *types.Transaction) []byte {
	val, err := types.InterfaceToBytes(tx)
	require.NoError(t, err)
	return val
}

func assertSimpleMessage(t *testing.T, respBody, expectedValue string) {
	var msg pb.SimpleMessage
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, expectedValue, msg.Value)
}

func TestSpacemeshGrpcService_GetTransaction(t *testing.T) {
	shutDown := launchServer(t)

	tx1 := genTx(t)
	txAPI.returnTx[tx1.ID()] = tx1

	tx2 := genTx(t)
	txAPI.returnTx[tx2.ID()] = tx2
	layerApplied := types.LayerID(1)
	txAPI.layerApplied[tx2.ID()] = &layerApplied

	tx3 := genTx(t)
	txAPI.returnTx[tx3.ID()] = tx3
	ap.nonces[tx3.Origin()] = 2222

	submitTx(t, tx1)
	submitTx(t, tx2)
	submitTx(t, tx3)

	respTx1 := getTx(t, tx1)
	respTx2 := getTx(t, tx2)
	respTx3 := getTx(t, tx3)

	assertTx(t, respTx1, tx1, "PENDING", 0, 0)
	assertTx(t, respTx2, tx2, "CONFIRMED", 1, genTimeUnix+layerDuration*2)
	assertTx(t, respTx3, tx3, "REJECTED", 0, 0)

	shutDown()
}

func getTx(t *testing.T, tx *types.Transaction) *pb.Transaction {
	r := require.New(t)
	idToSend := pb.TransactionId{Id: tx.ID().Bytes()}
	respBody, respStatus := callEndpoint(t, "v1/gettransaction", marshalProto(t, &idToSend))
	r.Equal(http.StatusOK, respStatus)
	var respTx pb.Transaction
	err := jsonpb.UnmarshalString(respBody, &respTx)
	r.NoError(err)
	return &respTx
}

func assertTx(t *testing.T, respTx *pb.Transaction, tx *types.Transaction, status string, layerID, timestamp uint64) {
	r := require.New(t)
	r.Equal(tx.ID().Bytes(), respTx.TxId.Id)
	r.Equal(tx.Fee, respTx.Fee)
	r.Equal(tx.Amount, respTx.Amount)
	r.Equal(util.Bytes2Hex(tx.Recipient.Bytes()), respTx.Receiver.Address)
	r.Equal(util.Bytes2Hex(tx.Origin().Bytes()), respTx.Sender.Address)
	r.Equal(layerID, respTx.LayerId)
	r.Equal(status, respTx.Status.String())
	r.Equal(timestamp, respTx.Timestamp)
}

func submitTx(t *testing.T, tx *types.Transaction) {
	r := require.New(t)

	txToSend := pb.SignedTransaction{Tx: asBytes(t, tx)}
	respBody, respStatus := callEndpoint(t, "v1/submittransaction", marshalProto(t, &txToSend))
	r.Equal(http.StatusOK, respStatus, http.StatusText(respStatus))

	txConfirmation := pb.TxConfirmation{}
	r.NoError(jsonpb.UnmarshalString(respBody, &txConfirmation))

	r.Equal("ok", txConfirmation.Value)
	r.Equal(tx.ID().String()[2:], txConfirmation.Id)
	r.Equal(asBytes(t, tx), networkMock.GetBroadcast())
}

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

func launchServer(t *testing.T) func() {
	networkMock.Broadcast("", []byte{0x00})
	defaultConfig := config2.DefaultConfig()
	grpcService := NewGrpcService(cfg.GrpcServerPort, &networkMock, ap, txAPI, txMempool, &mining, &oracle, &genTime, PostMock{}, layerDuration, &SyncerMock{}, &defaultConfig, nil)
	jsonService := NewJSONHTTPServer(cfg.JSONServerPort, cfg.GrpcServerPort)
	// start gRPC and json server
	grpcService.StartService()
	jsonService.StartService()

	time.Sleep(3 * time.Second) // wait for server to be ready (critical on Travis)

	return func() {
		require.NoError(t, jsonService.Close())
		require.NoError(t, grpcService.Close())
	}
}

func callEndpoint(t *testing.T, endpoint, payload string) (string, int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.JSONServerPort, endpoint)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return string(buf), resp.StatusCode
}

func genTx(t *testing.T) *types.Transaction {
	_, key, err := ed25519.GenerateKey(crand.Reader)
	require.NoError(t, err)
	signer, err := signing.NewEdSignerFromBuffer(key)
	require.NoError(t, err)
	tx, err := types.NewSignedTx(1111, [20]byte{}, 1234, 11, 321, signer)
	require.NoError(t, err)

	return tx
}

func TestJsonWalletApi_Errors(t *testing.T) {
	shutDown := launchServer(t)

	// generate request payload (api input params)
	addrBytes := []byte{0x02} // address that does not exist
	payload := marshalProto(t, &pb.AccountId{Address: util.Bytes2Hex(addrBytes)})
	const expectedResponse = "{\"error\":\"account does not exist\",\"message\":\"account does not exist\",\"code\":2}"
	expected := res{}
	res1 := res{}
	res2 := res{}
	json.Unmarshal([]byte(expectedResponse), &expected)

	respBody, respStatus := callEndpoint(t, "v1/nonce", payload)
	require.Equal(t, http.StatusInternalServerError, respStatus) // TODO: Should we change it to err 400 somehow?
	json.Unmarshal([]byte(respBody), &res1)
	require.Equal(t, expected, res1)

	respBody, respStatus = callEndpoint(t, "v1/balance", payload)
	require.Equal(t, http.StatusInternalServerError, respStatus) // TODO: Should we change it to err 400 somehow?
	json.Unmarshal([]byte(respBody), &res2)
	require.Equal(t, expected, res2)

	// stop the services
	shutDown()
}

func TestSpaceMeshGrpcService_Broadcast(t *testing.T) {
	shutDown := launchServer(t)

	// generate request payload (api input params)
	Data := "l33t"

	respBody, respStatus := callEndpoint(t, "v1/broadcast", marshalProto(t, &pb.BroadcastMessage{Data: Data}))
	require.Equal(t, http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, "ok")

	require.Equal(t, Data, string(networkMock.GetBroadcast()))

	// stop the services
	shutDown()
}

func TestSpaceMeshGrpcService_BroadcastErrors(t *testing.T) {
	shutDown := launchServer(t)
	networkMock.SetErr(true)
	const expectedResponse = "{\"error\":\"error during broadcast\",\"message\":\"error during broadcast\",\"code\":2}"
	expected := res{}
	json.Unmarshal([]byte(expectedResponse), &expected)

	Data := "l337"

	respBody, respStatus := callEndpoint(t, "v1/broadcast", marshalProto(t, &pb.BroadcastMessage{Data: Data}))
	require.Equal(t, http.StatusInternalServerError, respStatus) // TODO: Should we change it to err 400 somehow?
	res1 := res{}
	json.Unmarshal([]byte(respBody), &res1)
	require.Equal(t, expected, res1)

	require.NotEqual(t, Data, string(networkMock.GetBroadcast()))

	// stop the services
	shutDown()
}

type mockSrv struct {
	c      chan service.GossipMessage
	called bool
}

func (m *mockSrv) RegisterGossipProtocol(string, priorityq.Priority) chan service.GossipMessage {
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
	someKey := p2pcrypto.NewRandomPubkey()
	msg := &mockMsg{someKey, []byte("TEST"), make(chan service.MessageValidation, 1)}
	m.c <- msg
	res := <-msg.ValidationCompletedChan()
	require.NotNil(t, res)
	require.Equal(t, res.Sender(), someKey)
	require.Equal(t, res.Message(), []byte("TEST"))
	require.Equal(t, res.Protocol(), apiGossipProtocol)
	cancel()
}
