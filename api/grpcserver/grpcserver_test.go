package grpcserver

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"math"
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

const (
	miningStatus          = 123
	remainingBytes        = 321
	defaultGasLimit       = 10
	defaultFee            = 1
	genTimeUnix           = 1000000
	layerDurationSec      = 10
	layerAvgSize          = 10
	txsPerBlock           = 99
	TxReturnLayer         = 1
	layersPerEpoch        = 5
	networkID             = 120
	postGenesisEpochLayer = 22
)

var (
	networkMock = NetworkMock{}
	genTime     = GenesisTimeMock{time.Unix(genTimeUnix, 0)}
	txAPI       = &TxAPIMock{
		returnTx:     make(map[types.TransactionID]*types.Transaction),
		layerApplied: make(map[types.TransactionID]*types.LayerID),
	}
	coinbase    = types.HexToAddress("33333")
	pub, _, _   = ed25519.GenerateKey(nil)
	nodeID      = types.NodeID{Key: util.Bytes2Hex(pub), VRFPublicKey: []byte("22222")}
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	block1      = types.NewExistingBlock(0, []byte("11111"))
	block2      = types.NewExistingBlock(0, []byte("22222"))
	block3      = types.NewExistingBlock(0, []byte("33333"))
	defaultView = []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	chlng       = types.HexToHash32("55555")
	poetRef     = []byte("66666")
	npst        = activation.NewNIPSTWithChallenge(&chlng, poetRef)
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

// latest layer received
func (t *TxAPIMock) LatestLayer() types.LayerID {
	return 10
}

// latest layer approved/confirmed/applied to state
func (t *TxAPIMock) LatestLayerInState() types.LayerID {
	return 8
}

func (t *TxAPIMock) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	return t.layerApplied[txID]
}

func (t *TxAPIMock) GetTransaction(id types.TransactionID) (*types.Transaction, error) {
	return t.returnTx[id], nil
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

var globalBlock = types.NewExistingBlock(types.LayerID(0), []byte(rand.String(8)))

func (t *TxAPIMock) GetLayer(tid types.LayerID) (*types.Layer, error) {
	var blk1, blk2, blk3 *types.Block

	if tid > genTime.GetCurrentLayer() {
		return nil, errors.New("requested layer later than current layer")
	} else if tid > t.LatestLayer() {
		return nil, errors.New("haven't received that layer yet")
	}

	// Messy but this allows easier instrumentation
	if tid == 0 {
		blk1 = globalBlock
		blk2 = globalBlock
		blk3 = globalBlock
	} else {
		blk1 = types.NewExistingBlock(tid, []byte(rand.String(8)))
		blk2 = types.NewExistingBlock(tid, []byte(rand.String(8)))
		blk3 = types.NewExistingBlock(tid, []byte(rand.String(8)))
	}
	blocks := []*types.Block{blk1, blk2, blk3}
	return types.NewExistingLayer(tid, blocks), nil
}

var (
	challenge = newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	globalAtx = newAtx(challenge, 5, defaultView, npst)
)

func (t *TxAPIMock) GetATXs([]types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID) {
	atxs := map[types.ATXID]*types.ActivationTx{globalAtx.ID(): globalAtx}
	return atxs, nil
}

var tx1 *types.Transaction

func (t *TxAPIMock) GetTransactions([]types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
	return []*types.Transaction{tx1}, nil
}

func NewTx(nonce uint64, recipient types.Address, signer *signing.EdSigner) (*types.Transaction, error) {
	tx, err := mesh.NewSignedTx(nonce, recipient, 1, defaultGasLimit, defaultFee, signer)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func newChallenge(nodeID types.NodeID, sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPSTChallenge {
	challenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
	}
	return challenge
}

func newAtx(challenge types.NIPSTChallenge, ActiveSetSize uint32, View []types.BlockID, nipst *types.NIPST) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPSTChallenge: challenge,
				Coinbase:       coinbase,
				ActiveSetSize:  ActiveSetSize,
			},
			Nipst: nipst,
			View:  View,
		},
	}
	activationTx.CalcAndSetID()
	return activationTx
}

// MiningAPIMock is a mock for mining API
type MiningAPIMock struct{}

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
	return 12
}

func (t GenesisTimeMock) GetGenesisTime() time.Time {
	return t.t
}

type PostMock struct {
}

func (PostMock) Reset() error {
	return nil
}

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

var cfg = config.DefaultConfig()

type SyncerMock struct {
	startCalled bool
}

func (SyncerMock) IsSynced() bool { return false }
func (s *SyncerMock) Start()      { s.startCalled = true }

func launchServer(t *testing.T, services ...ServiceAPI) func() {
	networkMock.Broadcast("", []byte{0x00})
	grpcService := NewServer(cfg.NewGrpcServerPort)
	jsonService := NewJSONHTTPServer(cfg.NewJSONServerPort, cfg.NewGrpcServerPort)

	// attach services
	for _, svc := range services {
		svc.RegisterService(grpcService)
	}

	// start gRPC and json servers
	grpcService.Start()
	jsonService.StartService(cfg.StartNodeService, cfg.StartMeshService)
	time.Sleep(3 * time.Second) // wait for server to be ready (critical on Travis)

	return func() {
		require.NoError(t, jsonService.Close())
		grpcService.Close()
	}
}

func callEndpoint(t *testing.T, endpoint, payload string) (string, int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.NewJSONServerPort, endpoint)
	t.Log("sending POST request to", url)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	t.Log("got response", resp)
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

	grpcService := NewServer(port1)
	jsonService := NewJSONHTTPServer(port2, port1)

	require.Equal(t, port2, jsonService.Port, "Expected same port")
	require.Equal(t, port1, jsonService.GrpcPort, "Expected same port")
	require.Equal(t, port1, grpcService.Port, "Expected same port")
}

func TestNodeService(t *testing.T) {
	syncer := SyncerMock{}
	grpcService := NewNodeService(&networkMock, txAPI, &genTime, &syncer)
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
	c := pb.NewNodeServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"Echo", func(t *testing.T) {
			const message = "Hello World"
			res, err := c.Echo(context.Background(), &pb.EchoRequest{
				Msg: &pb.SimpleString{Value: message}})
			require.NoError(t, err)
			require.Equal(t, message, res.Msg.Value)

			// now try sending bad payloads
			_, err = c.Echo(context.Background(), &pb.EchoRequest{Msg: nil})
			require.EqualError(t, err, "rpc error: code = InvalidArgument desc = Must include `Msg`")
			code := status.Code(err)
			require.Equal(t, codes.InvalidArgument, code)

			_, err = c.Echo(context.Background(), &pb.EchoRequest{})
			require.EqualError(t, err, "rpc error: code = InvalidArgument desc = Must include `Msg`")
			code = status.Code(err)
			require.Equal(t, codes.InvalidArgument, code)
		}},
		{"Version", func(t *testing.T) {
			// must set this manually as it's set up in main() when running
			version := "abc123"
			cmd.Version = version
			res, err := c.Version(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, version, res.VersionString.Value)
		}},
		{"Build", func(t *testing.T) {
			// must set this manually as it's set up in main() when running
			build := "abc123"
			cmd.Commit = build
			res, err := c.Build(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, build, res.BuildString.Value)
		}},
		{"Status", func(t *testing.T) {
			req := &pb.StatusRequest{}
			res, err := c.Status(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, uint64(0), res.Status.ConnectedPeers)
			require.Equal(t, false, res.Status.IsSynced)
			require.Equal(t, uint64(10), res.Status.SyncedLayer)
			require.Equal(t, uint64(12), res.Status.TopLayer)
			require.Equal(t, uint64(8), res.Status.VerifiedLayer)
		}},
		{"SyncStart", func(t *testing.T) {
			require.Equal(t, false, syncer.startCalled, "Start() not yet called on syncer")
			req := &pb.SyncStartRequest{}
			res, err := c.SyncStart(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, true, syncer.startCalled, "Start() was called on syncer")
		}},
		{"Shutdown", func(t *testing.T) {
			called := false
			cmd.Cancel = func() { called = true }
			require.Equal(t, false, called, "cmd.Shutdown() not yet called")
			req := &pb.ShutdownRequest{}
			res, err := c.Shutdown(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, true, called, "cmd.Shutdown() was called")
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestMeshService(t *testing.T) {
	grpcService := NewMeshService(&networkMock, txAPI, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
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

	// Some shared test data
	layerFirst := 0
	layerLatestReceived := txAPI.LatestLayer()
	layerConfirmed := txAPI.LatestLayerInState()
	layerCurrent := genTime.GetCurrentLayer()

	// Generate some tx data that we can test
	tx, err := NewTx(1, types.BytesToAddress([]byte{0x02}), signing.NewEdSigner())
	require.NoError(t, err, "error generating test tx")
	tx1 = tx

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"GenesisTime", func(t *testing.T) {
			response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), response.Unixtime.Value)
		}},
		{"CurrentLayer", func(t *testing.T) {
			response, err := c.CurrentLayer(context.Background(), &pb.CurrentLayerRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(12), response.Layernum.Value)
		}},
		{"CurrentEpoch", func(t *testing.T) {
			response, err := c.CurrentEpoch(context.Background(), &pb.CurrentEpochRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(2), response.Epochnum.Value)
		}},
		{"NetId", func(t *testing.T) {
			response, err := c.NetID(context.Background(), &pb.NetIDRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(networkID), response.Netid.Value)
		}},
		{"LayerDuration", func(t *testing.T) {
			response, err := c.LayerDuration(context.Background(), &pb.LayerDurationRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerDurationSec), response.Duration.Value)
		}},
		{"MaxTransactionsPerSecond", func(t *testing.T) {
			response, err := c.MaxTransactionsPerSecond(context.Background(), &pb.MaxTransactionsPerSecondRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerAvgSize*txsPerBlock/layerDurationSec), response.Maxtxpersecond.Value)
		}},
		{"AccountMeshDataQuery", func(t *testing.T) {
			subtests := []struct {
				name string
				run  func(*testing.T)
			}{
				{
					// all inputs default to zero, no filter
					// query is valid but MaxResults is 0 so expect no results
					name: "no inputs",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "MinLayer too high",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MinLayer: layerCurrent.Uint64() + 1,
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "Offset too high",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							Offset: uint32(layerCurrent) + 1,
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "no filter",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "empty filter",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter:     &pb.AccountMeshDataFilter{},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with empty AccountId",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{},
							},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: tx.Origin().Bytes()},
							},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags zero",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: tx.Origin().Bytes()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_UNSPECIFIED),
							},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags tx only",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: tx.Origin().Bytes()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags activations only",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: tx.Origin().Bytes()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags all",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: tx.Origin().Bytes()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, 0, res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
			}
			//response, err := c.MaxTransactionsPerSecond(context.Background(), &pb.MaxTransactionsPerSecondRequest{})
			//require.NoError(t, err)
			//require.Equal(t, uint64(layerAvgSize*txsPerBlock/layerDurationSec), response.Maxtxpersecond.Value)
			// Run sub-subtests
			for _, r := range subtests {
				t.Run(r.name, r.run)
			}
		}},
		{"LayersQuery", func(t *testing.T) {
			generateRunFn := func(numResults int, req *pb.LayersQueryRequest) func(*testing.T) {
				return func(*testing.T) {
					res, err := c.LayersQuery(context.Background(), req)
					require.NoError(t, err, "query returned an unexpected error")
					require.Equal(t, numResults, len(res.Layer), "unexpected number of layer results")
				}
			}
			generateRunFnError := func(msg string, req *pb.LayersQueryRequest) func(*testing.T) {
				return func(*testing.T) {
					_, err := c.LayersQuery(context.Background(), req)
					require.Error(t, err, "expected query to produce an error")
					require.Contains(t, err.Error(), msg, "expected error to contain string")
				}
			}
			requests := []struct {
				name string
				run  func(*testing.T)
			}{
				// ERROR INPUTS
				// We expect these to produce errors

				// end layer after current layer
				{
					name: "end layer after current layer",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(layerCurrent),
						EndLayer:   uint32(layerCurrent + 2),
					}),
				},

				// start layer after current layer
				{
					name: "start layer after current layer",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(layerCurrent + 2),
						EndLayer:   uint32(layerCurrent + 3),
					}),
				},

				// layer after last received
				{
					name: "layer after last received",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(layerLatestReceived + 1),
						EndLayer:   uint32(layerLatestReceived + 2),
					}),
				},

				// very very large range
				{
					name: "very very large range",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(0),
						EndLayer:   uint32(math.MaxUint32),
					}),
				},

				// GOOD INPUTS

				// nil inputs
				// not an error since these default to zero, see
				// https://github.com/spacemeshos/api/issues/87
				{
					name: "nil inputs",
					run:  generateRunFn(1, &pb.LayersQueryRequest{}),
				},

				// start layer after end layer: expect no error, zero results
				{
					name: "start layer after end layer",
					run: generateRunFn(0, &pb.LayersQueryRequest{
						StartLayer: uint32(layerCurrent + 1),
						EndLayer:   uint32(layerCurrent),
					}),
				},

				// same start/end layer: expect no error, one result
				{
					name: "same start end layer",
					run: generateRunFn(1, &pb.LayersQueryRequest{
						StartLayer: uint32(layerLatestReceived),
						EndLayer:   uint32(layerLatestReceived),
					}),
				},

				// start layer after last approved/confirmed layer (but before current layer)
				{
					name: "start layer after last approved confirmed layer",
					run: generateRunFn(2, &pb.LayersQueryRequest{
						StartLayer: uint32(layerConfirmed + 1),
						EndLayer:   uint32(layerConfirmed + 2),
					}),
				},

				// end layer after last approved/confirmed layer (but before current layer)
				{
					name: "end layer after last approved confirmed layer",
					// expect difference + 1 return layers
					run: generateRunFn(int(layerConfirmed)+2-layerFirst+1, &pb.LayersQueryRequest{
						StartLayer: uint32(layerFirst),
						EndLayer:   uint32(layerConfirmed + 2),
					}),
				},

				// comprehensive valid test
				{
					name: "comprehensive",
					run: func(t *testing.T) {
						req := &pb.LayersQueryRequest{
							StartLayer: uint32(layerFirst),
							EndLayer:   uint32(layerLatestReceived),
						}

						res, err := c.LayersQuery(context.Background(), req)
						require.NoError(t, err, "query returned unexpected error")

						resLayer := res.Layer[0]
						require.Equal(t, uint64(0), resLayer.Number, "first layer is zero")
						require.Equal(t, pb.Layer_LAYER_STATUS_CONFIRMED, resLayer.Status, "first layer is confirmed")

						resLayerNine := res.Layer[9]
						require.Equal(t, uint64(9), resLayerNine.Number, "layer nine is ninth")
						require.Equal(t, pb.Layer_LAYER_STATUS_UNSPECIFIED, resLayerNine.Status, "later layer is unconfirmed")

						// endpoint inclusive so add one
						numLayers := int(layerLatestReceived) - layerFirst + 1
						numTxPerBlock := 1
						numAtxPerLayer := 1
						numBlkPerLayer := 3
						//numTxs := numLayers * numTxPerLayer
						//numAtxs := numLayers * numAtxPerLayer
						require.Equal(t, numLayers, len(res.Layer))

						require.Equal(t, numAtxPerLayer, len(resLayer.Activations))
						require.Equal(t, numBlkPerLayer, len(resLayer.Blocks))

						resActivation := resLayer.Activations[0]
						require.Equal(t, globalAtx.ID().Bytes(), resActivation.Id.Id)
						require.Equal(t, resLayer.Number, resActivation.Layer)
						require.Equal(t, globalAtx.NodeID.ToBytes(), resActivation.SmesherId.Id)
						require.Equal(t, globalAtx.Coinbase.Bytes(), resActivation.Coinbase.Address)
						require.Equal(t, globalAtx.PrevATXID.Bytes(), resActivation.PrevAtx.Id)
						data, err := globalAtx.InnerBytes()
						require.NoError(t, err)
						require.Equal(t, uint64(len(data)), resActivation.CommitmentSize)

						resBlock := resLayer.Blocks[0]

						require.Equal(t, numTxPerBlock, len(resBlock.Transactions))
						require.Equal(t, globalBlock.ID().Bytes(), resBlock.Id)

						// Check the tx as well
						resTx := resBlock.Transactions[0]
						require.Equal(t, tx.ID().Bytes(), resTx.Id.Id)
						require.Equal(t, tx.Origin().Bytes(), resTx.Sender.Address)
						require.Equal(t, tx.GasLimit, resTx.GasOffered.GasProvided)
						require.Equal(t, tx.Fee, resTx.GasOffered.GasPrice)
						require.Equal(t, tx.Amount, resTx.Amount.Value)
						require.Equal(t, tx.AccountNonce, resTx.Counter)
						require.Equal(t, tx.Signature[:], resTx.Signature.Signature)
						require.Equal(t, pb.Signature_SCHEME_ED25519_PLUS_PLUS, resTx.Signature.Scheme)
						require.Equal(t, tx.Origin().Bytes(), resTx.Signature.PublicKey)

						// The Data field is a bit trickier to read
						switch x := resTx.Data.(type) {
						case *pb.Transaction_CoinTransfer:
							require.Equal(t, tx.Recipient.Bytes(), x.CoinTransfer.Receiver.Address,
								"inner coin transfer tx has bad recipient")
						default:
							require.Fail(t, "inner tx has wrong tx data type")
						}
					},
				},
			}

			// Run sub-subtests
			for _, r := range requests {
				t.Run(r.name, r.run)
			}
		}},
	}

	// Run subtests
	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestMultiService(t *testing.T) {
	svc1 := NewNodeService(&networkMock, txAPI, &genTime, &SyncerMock{})
	svc2 := NewMeshService(&networkMock, txAPI, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
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
	c1 := pb.NewNodeServiceClient(conn)
	c2 := pb.NewMeshServiceClient(conn)

	// call endpoints and validate results
	const message = "Hello World"
	res1, err1 := c1.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	require.NoError(t, err1)
	require.Equal(t, message, res1.Msg.Value)
	res2, err2 := c2.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
	require.NoError(t, err2)
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), res2.Unixtime.Value)

	// Make sure that shutting down the grpc service shuts them both down
	shutDown()

	// Make sure NodeService is off
	res1, err1 = c1.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	require.Error(t, err1)
	require.Contains(t, err1.Error(), "rpc error: code = Unavailable")

	// Make sure MeshService is off
	res2, err2 = c2.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
	require.Error(t, err2)
	require.Contains(t, err2.Error(), "rpc error: code = Unavailable")
}

func TestJsonApi(t *testing.T) {
	const message = "hello world!"

	// we cannot start the gateway service without enabling at least one service
	require.Equal(t, cfg.StartNodeService, false)
	require.Equal(t, cfg.StartMeshService, false)
	shutDown := launchServer(t)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.NewJSONServerPort, "v1/node/echo")
	t.Log("sending POST request to", url)
	_, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf(
		"dial tcp 127.0.0.1:%d: connect: connection refused",
		cfg.NewJSONServerPort))
	shutDown()

	// enable services and try again
	svc1 := NewNodeService(&networkMock, txAPI, &genTime, &SyncerMock{})
	svc2 := NewMeshService(&networkMock, txAPI, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
	cfg.StartNodeService = true
	cfg.StartMeshService = true
	shutDown = launchServer(t, svc1, svc2)
	defer shutDown()

	// generate request payload (api input params)
	payload = marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	respBody, respStatus := callEndpoint(t, "v1/node/echo", payload)
	require.Equal(t, http.StatusOK, respStatus)
	var msg pb.EchoResponse
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)

	// Test MeshService
	respBody2, respStatus2 := callEndpoint(t, "v1/mesh/genesistime", "")
	require.Equal(t, http.StatusOK, respStatus2)
	var msg2 pb.GenesisTimeResponse
	require.NoError(t, jsonpb.UnmarshalString(respBody2, &msg2))
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), msg2.Unixtime.Value)
}
