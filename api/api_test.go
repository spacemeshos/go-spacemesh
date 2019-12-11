package api

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	mockOrigin   types.Address
	returnTx     map[types.TransactionId]*types.Transaction
	layerApplied map[types.TransactionId]*types.LayerID
	err          error
}

func (t *TxAPIMock) ValidateNonceAndBalance(transaction *types.Transaction) error {
	return t.err
}

func (t *TxAPIMock) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	return prevNonce, prevBalance, nil
}

func (t *TxAPIMock) ValidatedLayer() types.LayerID {
	return ValidatedLayerId
}

func (t *TxAPIMock) GetLayerApplied(txId types.TransactionId) *types.LayerID {
	return t.layerApplied[txId]
}

func (t *TxAPIMock) GetTransaction(id types.TransactionId) (*types.Transaction, error) {
	return t.returnTx[id], nil
}

func (t *TxAPIMock) LatestLayer() types.LayerID {
	return 10
}

func (t *TxAPIMock) GetRewards(account types.Address) (rewards []types.Reward, err error) {
	return
}

func (t *TxAPIMock) GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionId) {
	if l != TxReturnLayer {
		return nil
	}
	for _, tx := range t.returnTx {
		if tx.Recipient.String() == account.String() {
			txs = append(txs, tx.Id())
		}
	}
	return
}

func (t *TxAPIMock) GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionId) {
	if l != TxReturnLayer {
		return nil
	}
	for _, tx := range t.returnTx {
		if tx.Origin().String() == account.String() {
			txs = append(txs, tx.Id())
		}
	}
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

type PostMock struct {
}

func (PostMock) Reset() error {
	return nil
}

const (
	genTimeUnix      = 1000000
	layerDuration    = 10
	ValidatedLayerId = 8
	TxReturnLayer    = 1
)

var (
	ap          = NewNodeAPIMock()
	networkMock = NetworkMock{}
	mining      = MinigApiMock{}
	oracle      = OracleMock{}
	genTime     = GenesisTimeMock{time.Unix(genTimeUnix, 0)}
	txMempool   = miner.NewTxMemPool()
	txApi       = &TxAPIMock{
		returnTx:     make(map[types.TransactionId]*types.Transaction),
		layerApplied: make(map[types.TransactionId]*types.LayerID),
	}
)

func TestServersConfig(t *testing.T) {
	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	require.NoError(t, err, "Should be able to establish a connection on a port")

	config.ConfigValues.JSONServerPort = port1
	config.ConfigValues.GrpcServerPort = port2
	grpcService := NewGrpcService(&networkMock, ap, txApi, nil, &mining, &oracle, nil, PostMock{}, 0, nil)
	jsonService := NewJSONHTTPServer()

	require.Equal(t, grpcService.Port, uint(config.ConfigValues.GrpcServerPort), "Expected same port")
	require.Equal(t, jsonService.Port, uint(config.ConfigValues.JSONServerPort), "Expected same port")
}

func TestGrpcApi(t *testing.T) {
	shutDown := launchServer(t)

	const message = "Hello World"

	// start a client
	addr := "localhost:" + strconv.Itoa(config.ConfigValues.GrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer conn.Close()
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

	r.Equal([]byte{1, 2, 3}, networkMock.broadcasted)

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
	txApi.err = fmt.Errorf(msg)
	respBody, respStatus = callEndpoint(t, "v1/submittransaction", marshalProto(t, &txToSend))
	txApi.err = nil
	r.Equal(http.StatusInternalServerError, respStatus, http.StatusText(respStatus))
	r.Equal("{\"error\":\""+msg+"\",\"message\":\""+msg+"\",\"code\":2}", respBody)

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

	r.Equal(int32(1), stats.Status)
	r.Equal("/tmp", stats.DataDir)
	r.Equal("123456", stats.Coinbase)

	// test get genesisTime
	respBody, respStatus = callEndpoint(t, "v1/genesis", "")
	r.Equal(http.StatusOK, respStatus)
	assertSimpleMessage(t, respBody, genTime.t.String())

	// test get rewards per account
	payload = marshalProto(t, &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())})
	respBody, respStatus = callEndpoint(t, "v1/accountrewards", payload)
	r.Equal(http.StatusOK, respStatus)

	var rewards pb.AccountRewards
	r.NoError(jsonpb.UnmarshalString(respBody, &rewards))
	r.Empty(rewards.Rewards) // TODO: Test with actual data returned

	// test get txs per account:

	// add incoming tx to mempool
	mempoolTxIn, err := mesh.NewSignedTx(1337, addr, 420, 3, 42, signing.NewEdSigner())
	r.NoError(err)
	txMempool.Put(mempoolTxIn.Id(), mempoolTxIn)

	// add outgoing tx to mempool
	mempoolTxOut, err := mesh.NewSignedTx(1337, types.BytesToAddress([]byte{1}), 420, 3, 42, signer)
	r.NoError(err)
	txMempool.Put(mempoolTxOut.Id(), mempoolTxOut)

	// add incoming tx to mesh
	meshTxIn, err := mesh.NewSignedTx(1337, addr, 420, 3, 42, signing.NewEdSigner())
	r.NoError(err)
	txApi.returnTx[meshTxIn.Id()] = meshTxIn

	// add outgoing tx to mesh
	meshTxOut, err := mesh.NewSignedTx(1337, types.BytesToAddress([]byte{1}), 420, 3, 42, signer)
	r.NoError(err)
	txApi.returnTx[meshTxOut.Id()] = meshTxOut

	// test with start layer that gets the mesh txs
	payload = marshalProto(t, &pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())}, StartLayer: TxReturnLayer})
	respBody, respStatus = callEndpoint(t, "v1/accounttxs", payload)
	r.Equal(http.StatusOK, respStatus)

	var accounts pb.AccountTxs
	r.NoError(jsonpb.UnmarshalString(respBody, &accounts))
	r.Equal(uint64(ValidatedLayerId), accounts.ValidatedLayer)
	r.ElementsMatch([]string{
		mempoolTxIn.Id().String(),
		mempoolTxOut.Id().String(),
		meshTxIn.Id().String(),
		meshTxOut.Id().String(),
	}, accounts.Txs)

	// test with start layer that doesn't get the mesh txs (mempool txs return anyway)
	payload = marshalProto(t, &pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())}, StartLayer: TxReturnLayer + 1})
	respBody, respStatus = callEndpoint(t, "v1/accounttxs", payload)
	r.Equal(http.StatusOK, respStatus)

	r.NoError(jsonpb.UnmarshalString(respBody, &accounts))
	r.Equal(uint64(ValidatedLayerId), accounts.ValidatedLayer)
	r.ElementsMatch([]string{
		mempoolTxIn.Id().String(),
		mempoolTxOut.Id().String(),
	}, accounts.Txs)

	// test get txs per account with wrong layer error
	payload = marshalProto(t, &pb.GetTxsSinceLayer{Account: &pb.AccountId{Address: util.Bytes2Hex(addr.Bytes())}, StartLayer: 11})
	respBody, respStatus = callEndpoint(t, "v1/accounttxs", payload)
	r.Equal(http.StatusInternalServerError, respStatus)
	const ErrInvalidStartLayer = "{\"error\":\"invalid start layer\",\"message\":\"invalid start layer\",\"code\":2}"
	r.Equal(ErrInvalidStartLayer, respBody)

	// test call reset post
	respBody, respStatus = callEndpoint(t, "v1/resetpost", "")
	r.Equal(http.StatusOK, respStatus)

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
	txApi.returnTx[tx1.Id()] = tx1

	tx2 := genTx(t)
	txApi.returnTx[tx2.Id()] = tx2
	layerApplied := types.LayerID(1)
	txApi.layerApplied[tx2.Id()] = &layerApplied

	tx3 := genTx(t)
	txApi.returnTx[tx3.Id()] = tx3
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

func getTx(t *testing.T, tx *types.Transaction) pb.Transaction {
	r := require.New(t)
	idToSend := pb.TransactionId{Id: tx.Id().Bytes()}
	respBody, respStatus := callEndpoint(t, "v1/gettransaction", marshalProto(t, &idToSend))
	r.Equal(http.StatusOK, respStatus)
	var respTx pb.Transaction
	err := jsonpb.UnmarshalString(respBody, &respTx)
	r.NoError(err)
	return respTx
}

func assertTx(t *testing.T, respTx pb.Transaction, tx *types.Transaction, status string, layerId, timestamp uint64) {
	r := require.New(t)
	r.Equal(tx.Id().Bytes(), respTx.TxId.Id)
	r.Equal(tx.Fee, respTx.Fee)
	r.Equal(tx.Amount, respTx.Amount)
	r.Equal(util.Bytes2Hex(tx.Recipient.Bytes()), respTx.Receiver.Address)
	r.Equal(util.Bytes2Hex(tx.Origin().Bytes()), respTx.Sender.Address)
	r.Equal(layerId, respTx.LayerId)
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
	r.Equal(tx.Id().String()[2:], txConfirmation.Id)
	r.Equal(asBytes(t, tx), networkMock.broadcasted)
}

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

func launchServer(t *testing.T) func() {
	port1, err := node.GetUnboundedPort()
	require.NoError(t, err)
	port2, err := node.GetUnboundedPort()
	require.NoError(t, err)
	if config.ConfigValues.JSONServerPort == 0 {
		config.ConfigValues.JSONServerPort = port1
		config.ConfigValues.GrpcServerPort = port2
	}
	networkMock.broadcasted = []byte{0x00}
	grpcService := NewGrpcService(&networkMock, ap, txApi, txMempool, &mining, &oracle, &genTime, PostMock{}, layerDuration, nil)
	jsonService := NewJSONHTTPServer()
	// start gRPC and json server
	grpcService.StartService()
	jsonService.StartService()

	time.Sleep(3 * time.Second) // wait for server to be ready (critical on Travis)

	return func() {
		jsonService.StopService()
		grpcService.StopService()
	}
}

func callEndpoint(t *testing.T, endpoint, payload string) (string, int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", config.ConfigValues.JSONServerPort, endpoint)
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
	tx, err := mesh.NewSignedTx(1111, [20]byte{}, 1234, 11, 321, signer)
	require.NoError(t, err)

	return tx
}

func TestJsonWalletApi_Errors(t *testing.T) {
	shutDown := launchServer(t)

	// generate request payload (api input params)
	addrBytes := []byte{0x02} // address that does not exist
	payload := marshalProto(t, &pb.AccountId{Address: util.Bytes2Hex(addrBytes)})
	const expectedResponse = "{\"error\":\"account does not exist\",\"message\":\"account does not exist\",\"code\":2}"

	respBody, respStatus := callEndpoint(t, "v1/nonce", payload)
	require.Equal(t, http.StatusInternalServerError, respStatus) // TODO: Should we change it to err 400 somehow?
	require.Equal(t, expectedResponse, respBody)

	respBody, respStatus = callEndpoint(t, "v1/balance", payload)
	require.Equal(t, http.StatusInternalServerError, respStatus) // TODO: Should we change it to err 400 somehow?
	require.Equal(t, expectedResponse, respBody)

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

	require.Equal(t, Data, string(networkMock.broadcasted))

	// stop the services
	shutDown()
}

func TestSpaceMeshGrpcService_BroadcastErrors(t *testing.T) {
	shutDown := launchServer(t)
	networkMock.broadCastErr = true
	const expectedResponse = "{\"error\":\"error during broadcast\",\"message\":\"error during broadcast\",\"code\":2}"

	Data := "l337"

	respBody, respStatus := callEndpoint(t, "v1/broadcast", marshalProto(t, &pb.BroadcastMessage{Data: Data}))
	require.Equal(t, http.StatusInternalServerError, respStatus) // TODO: Should we change it to err 400 somehow?
	require.Equal(t, expectedResponse, respBody)

	require.NotEqual(t, Data, string(networkMock.broadcasted))

	// stop the services
	shutDown()
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
