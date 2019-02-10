package app

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/hare"
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

	go Main()

	<-EntryPointCreated

	assert.NotNil(t, App)

	<-App.NodeInitCallback

	assert.NotNil(t, App.P2P)
	assert.NotNil(t, App)
	assert.Equal(t, App.Config.API.StartJSONServer, true)

	// app should exit based on this signal
	Cancel()

	filesystem.DeleteSpacemeshDataFolders(t)

}

func (app *AppTestSuite) initMultipleInstances(t *testing.T, numOfInstances int) {
	net := service.NewSimulator()
	storeFormat := "../tmp/state_"
	runningName := 'a'
	bo := oracle.NewLocalOracle(numOfInstances)
	for i := 0; i < numOfInstances; i++ {
		app.apps = append(app.apps, newSpacemeshApp())
		store := storeFormat + string(runningName)
		n := net.NewNode()

		sgn := hare.NewMockSigning() //todo: shouldn't be any mock code here
		pub := sgn.Verifier()
		bo.Register(true, pub.String())

		err := app.apps[i].initServices(pub.String(), n, store, sgn, bo, bo)
		assert.NoError(t, err)
		app.apps[i].setupGenesis()
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

	app.initMultipleInstances(app.T(), 2)

	for _, a := range app.apps {
		a.startServices()
	}

	app.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(25 * time.Second)

	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			app.T().Error("timed out ")
		default:
			for _, ap := range app.apps {
				ok := 0

				if big.NewInt(10).Cmp(ap.state.GetBalance(dst)) == 0 {
					for _, ap2 := range app.apps {
						assert.Equal(app.T(), ap.state.IntermediateRoot(false), ap2.state.IntermediateRoot(false))
						if ap.state.IntermediateRoot(false) == ap2.state.IntermediateRoot(false) {
							ok++
						}
					}
					if ok == len(app.apps) {
						return
					}
				}
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func TestAppTestSuite(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
