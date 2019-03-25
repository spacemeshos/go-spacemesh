package main

import (
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spf13/cobra"
	"os"
	"time"
)

const defaultSetSize = 200

var lg = log.New("sync_test", "", "")

// Sync cmd
var Cmd = &cobra.Command{
	Use:   "sync",
	Short: "start sync",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting hare")
		syncApp := NewSyncApp()
		defer syncApp.Cleanup()
		syncApp.Initialize(cmd)
		syncApp.Start(cmd, args)
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

type SyncApp struct {
	*cmdp.BaseApp
	*sync.Syncer
}

func NewSyncApp() *SyncApp {
	return &SyncApp{BaseApp: cmdp.NewBaseApp()}
}

func (app *SyncApp) Cleanup() {

}

func getMesh() *mesh.Mesh {

	//time := time.Now()
	bdb := database.NewMemDatabase()
	ldb := database.NewMemDatabase()
	cdb := database.NewMemDatabase()
	layers := mesh.NewMesh(ldb, bdb, cdb, sync.ConfigTst(), &sync.MeshValidatorMock{}, sync.MockState{}, lg)
	return layers
}

func (app *SyncApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)

	if err != nil {
		panic("something got fuged")
	}

	conf := sync.Configuration{SyncInterval: 1 * time.Second, Concurrency: 4, LayerSize: int(5), RequestTimeout: 100 * time.Millisecond}
	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	clock := timesync.NewTicker(timesync.RealClock{}, ld, gTime)
	app.Syncer = sync.NewSync(swarm, getMesh(), sync.BlockValidatorMock{}, conf, clock.Subscribe(), lg)
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
