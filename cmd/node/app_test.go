package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	sync2 "github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/poet-ref/integration"
	"github.com/stretchr/testify/suite"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type AppTestSuite struct {
	suite.Suite

	apps []*SpacemeshApp
	dbs  []string
}

func (app *AppTestSuite) SetupTest() {
	app.apps = make([]*SpacemeshApp, 0, 0)
	app.dbs = make([]string, 0, 0)
}

// NewRPCPoetHarnessClient returns a new instance of RPCPoetClient
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
func NewRPCPoetHarnessClient() (*nipst.RPCPoetClient, error) {
	h, err := integration.NewHarness()
	if err != nil {
		return nil, err
	}

	return nipst.NewRPCPoetClient(h.PoetClient, h.TearDown), nil
}

func (app *AppTestSuite) TearDownTest() {
	for _, dbinst := range app.dbs {
		err := os.RemoveAll(dbinst)
		if err != nil {
			panic(fmt.Sprintf("what happened : %v", err))
		}
	}
	err := os.RemoveAll("../tmp")
	if err != nil {
		log.Error("error while cleaning up: %v", err)
	}
}

func (app *AppTestSuite) initMultipleInstances(t *testing.T, numOfInstances int, storeFormat string) {
	net := service.NewSimulator()
	runningName := 'a'
	rolacle := eligibility.New()
	poet, err := NewRPCPoetHarnessClient()
	assert.NoError(t, err)
	for i := 0; i < numOfInstances; i++ {
		smApp := NewSpacemeshApp()
		smApp.Config.HARE.N = numOfInstances
		smApp.Config.HARE.F = numOfInstances / 2

		sgn := hare.NewMockSigning() //todo: shouldn't be any mock code here
		pub := sgn.Verifier()

		nodeId := types.NodeId{Key: pub.String()}
		swarm := net.NewNode()
		dbStorepath := storeFormat + string(runningName)

		blockOracle := oracle.NewLocalOracle(rolacle, numOfInstances, nodeId)
		blockOracle.Register(true, pub.String())

		blockValidator := sync2.BlockValidatorMock{}
		hareOracle := blockOracle
		layerSize := numOfInstances

		err := smApp.initServices(nodeId, swarm, dbStorepath, sgn, blockOracle, blockValidator, hareOracle, layerSize, nipst.NewPostClient(), poet)
		assert.NoError(t, err)
		smApp.setupGenesis(apiCfg.DefaultGenesisConfig())

		app.apps = append(app.apps, smApp)
		app.dbs = append(app.dbs, dbStorepath)

		runningName++
	}
}

func (app *AppTestSuite) TestMultipleNodes() {
	//EntryPointCreated <- true

	addr := address.BytesToAddress([]byte{0x01})
	dst := address.BytesToAddress([]byte{0x02})
	tx := types.SerializableTransaction{}
	tx.Amount = big.NewInt(10).Bytes()
	tx.GasLimit = 1
	tx.Origin = addr
	tx.Recipient = &dst
	tx.Price = big.NewInt(1).Bytes()

	txbytes, _ := types.TransactionAsBytes(&tx)
	path := "../tmp/test/state_" + time.Now().String()
	app.initMultipleInstances(app.T(), 10, path)
	for _, a := range app.apps {
		a.startServices()
	}

	_ = app.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(2 * 60 * time.Second)

	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			app.T().Fatal("timed out ")
		default:
			for idx, ap := range app.apps {
				if big.NewInt(10).Cmp(ap.state.GetBalance(dst)) == 0 {
					clientsDone := 0
					for idx2, ap2 := range app.apps {
						if idx != idx2 {
							r1 := ap.state.IntermediateRoot(false).String()
							r2 := ap2.state.IntermediateRoot(false).String()
							if r1 == r2 {
								clientsDone++
								log.Info("%d roots confirmed out of %d", clientsDone, len(app.apps))
								if clientsDone == len(app.apps)-1 {
									app.gracefulShutdown()
									return
								}
							}
						}
					}

				}
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}

func (app *AppTestSuite) gracefulShutdown() {
	var wg sync.WaitGroup
	for _, ap := range app.apps {
		func(ap SpacemeshApp) {
			wg.Add(1)
			defer wg.Done()
			ap.stopServices()
		}(*ap)
	}
	wg.Wait()
}

func TestAppTestSuite(t *testing.T) {
	//defer leaktest.Check(t)()
	suite.Run(t, new(AppTestSuite))
}
