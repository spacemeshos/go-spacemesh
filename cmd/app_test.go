package cmd

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/suite"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
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

func (app *AppTestSuite) TearDownTest() {

	for _, ap := range app.apps {
		ap.stopServices()
	}

	for _, dbinst := range app.dbs {
		err := os.RemoveAll(dbinst)
		if err != nil {
			panic(fmt.Sprintf("what happened : %v", err))
		}
	}
}

func TestApp(t *testing.T) {
	t.Skip()
	filesystem.SetupTestSpacemeshDataFolders(t, "app_test")

	// remove all injected test flags for now
	os.Args = []string{"/go-spacemesh", "--json-server=true"}

	smapp := newSpacemeshApp()
	defer smapp.Cleanup(NodeCmd, os.Args)
	smapp.Initialize(NodeCmd, os.Args)
	smapp.Start(NodeCmd, os.Args)

	//<-EntryPointCreated

	assert.NotNil(t, App)

	<-App.NodeInitCallback

	assert.NotNil(t, App.P2P)
	assert.NotNil(t, App)
	assert.Equal(t, App.Config.API.StartJSONServer, true)

	// app should exit based on this signal
	Cancel()

	filesystem.DeleteSpacemeshDataFolders(t)

}

func (app *AppTestSuite) initMultipleInstances(t *testing.T, numOfInstances int, storeFormat string) {
	net := service.NewSimulator()
	runningName := 'a'
	bo := oracle.NewLocalOracle(numOfInstances)
	for i := 0; i < numOfInstances; i++ {
		smapp := newSpacemeshApp()
		smapp.Config.HARE.N = numOfInstances
		smapp.Config.HARE.F = numOfInstances / 2
		app.apps = append(app.apps, smapp)
		store := storeFormat + string(runningName)
		n := net.NewNode()

		sgn := hare.NewMockSigning() //todo: shouldn't be any mock code here
		pub := sgn.Verifier()
		bo.Register(true, pub.String())

		err := app.apps[i].initServices(pub.String(), n, store, sgn, bo, bo, uint32(numOfInstances))
		assert.NoError(t, err)
		app.apps[i].setupGenesis(apiCfg.DefaultGenesisConfig())
		app.dbs = append(app.dbs, store)
		runningName++
	}
}

func (app *AppTestSuite) TestMultipleNodes() {
	//EntryPointCreated <- true

	addr := address.BytesToAddress([]byte{0x01})
	dst := address.BytesToAddress([]byte{0x02})
	tx := mesh.SerializableTransaction{}
	tx.Amount = big.NewInt(10).Bytes()
	tx.GasLimit = 1
	tx.Origin = addr
	tx.Recipient = &dst
	tx.Price = big.NewInt(1).Bytes()

	txbytes, _ := mesh.TransactionAsBytes(&tx)
	path := "../tmp/test/state_" + time.Now().String()
	app.initMultipleInstances(app.T(), 10, path)
	for _, a := range app.apps {
		a.startServices()
	}

	app.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(2 * 60 * time.Second)

	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			app.T().Fatal("timed out ")
		default:
			for idx, ap := range app.apps {
				if big.NewInt(10).Cmp(ap.state.GetBalance(dst)) == 0 {
					ok := 0
					for idx2, ap2 := range app.apps {
						if idx != idx2 {
							r1 := ap.state.IntermediateRoot(false).String()
							r2 := ap2.state.IntermediateRoot(false).String()
							if r1 == r2 {
								log.Info("%d roots confirmed out of %d", ok, len(app.apps))
								ok++
							}
						}
						if ok == len(app.apps)-1 {
							return
						}
					}

				}
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}

func TestAppTestSuite(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
