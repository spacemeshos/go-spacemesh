package app

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
)

func TestApp(t *testing.T) {
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

func TestMultipleNodes(t *testing.T){
	net := service.NewSimulator()


	n := net.NewNode()

	receiver := net.NewNode()

	App = newSpacemeshApp()

	EntryPointCreated <- true

	addr := common.BytesToAddress([]byte{0x01})
	dst := common.BytesToAddress([]byte{0x01})
	tx := mesh.SerializableTransaction{}
	tx.Amount = big.NewInt(10).Bytes()
	tx.GasLimit =1
	tx.Origin = addr
	tx.Recipient = &dst
	tx.Price = big.NewInt(1).Bytes()

	txbytes, _ := mesh.TransactionAsBytes(&tx)
	n.Broadcast(miner.TxGossipChannel, txbytes)
	_ ,err := App.initAndStartServices("a", n)
	assert.NoError(t, err)
	st2, err := App.initAndStartServices("b",receiver)
	assert.NoError(t, err)

	timeout := time.After(30 * time.Second)


	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if big.NewInt(10) == st2.GetBalance(dst) {
				return
			}
		}
	}


}
