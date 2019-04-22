package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"time"
)

// Sync cmd
var Cmd = &cobra.Command{
	Use:   "sync",
	Short: "start sync",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting sync")
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
	sync  *sync.Syncer
	clock *timesync.Ticker
}

func NewSyncApp() *SyncApp {
	return &SyncApp{BaseApp: cmdp.NewBaseApp()}
}

func (app *SyncApp) Cleanup() {

}

func IOReadDir(root string) ([]string, error) {
	var files []string
	fileInfo, err := ioutil.ReadDir(root)
	if err != nil {
		return files, err
	}
	for _, file := range fileInfo {
		files = append(files, file.Name())
	}
	return files, nil
}

func (app *SyncApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	lg := log.New("sync_test", "", "")
	lg.Info("Start!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	lg.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)

	files, _ := IOReadDir(app.Config.DataDir)

	lg.Info("path %v", app.Config.DataDir)
	lg.Info("files %v", files)
	if err != nil {
		panic("something got fudged while creating p2p service ")
	}
	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	app.clock = timesync.NewTicker(timesync.RealClock{}, time.Duration(app.Config.LayerDurationSec)*time.Second, gTime)
	lg.Info("Genesis %v", gTime)

	mshDb := mesh.NewPersistentMeshDB(app.Config.DataDir, lg)
	msh := mesh.NewMesh(mshDb,
		activation.NewActivationDb(database.NewMemDatabase(), mshDb, 1000),
		sync.ConfigTst(),
		&sync.MeshValidatorMock{},
		&sync.MockState{},
		lg)

	defer msh.Close()
	i := 0
	for ; ; i++ {
		if lyr, err := mshDb.GetLayer(types.LayerID(i)); err != nil || lyr == nil {
			lg.Info("loaded %v layers from disk %v", i-1, err)
			break
		} else {
			lg.Info("loaded layer %v from disk ", i)
			mshDb.AddLayer(lyr)
		}
	}
	msh.SetLatestLayer(types.LayerID(i - 1))
	app.clock.Start()

	conf := sync.Configuration{
		SyncInterval:   1 * time.Second,
		Concurrency:    4,
		LayerSize:      int(5),
		RequestTimeout: 100 * time.Millisecond,
	}

	app.sync = sync.NewSync(swarm, msh, sync.BlockValidatorMock{}, conf, make(chan types.LayerID), lg)
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}
	app.sync.Start()
	lg.Info("wait %v sec", time.Duration(app.Config.LayerDurationSec)*time.Second)
	time.Sleep(time.Duration(app.Config.LayerDurationSec) * time.Second)
	lg.Info("is synced = %v ", app.sync.IsSynced())

	for !app.sync.IsSynced() {
		lg.Info("waiting for sync to finish before shutting down")
		lg.Info("sleep for %v sec", 20)
		time.Sleep(20 * time.Second)
	}

	lg.Info("node %v done with %v verified layers", app.BaseApp.Config.P2P.NodeID, app.sync.VerifiedLayer())
}

func main() {
	if err := Cmd.Execute(); err != nil {
		log.Info("error ", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}
