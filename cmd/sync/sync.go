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
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)

	if err != nil {
		panic("something got fudged while creating p2p service ")
	}

	memesh := mesh.NewMemMeshDB(lg)
	atxdb := activation.NewActivationDb(database.NewMemDatabase(), activation.NewIdentityStore(database.NewMemDatabase()), memesh, 1000)
	mshDb := mesh.NewPersistentMeshDB(app.Config.DataDir, lg)
	msh := mesh.NewMesh(mshDb,
		atxdb,
		sync.ConfigTst(),
		&sync.MeshValidatorMock{},
		&sync.MockState{},
		lg)
	defer msh.Close()
	conf := sync.Configuration{
		SyncInterval:   1 * time.Second,
		Concurrency:    4,
		LayerSize:      int(5),
		RequestTimeout: 100 * time.Millisecond,
	}

	ch := make(chan types.LayerID, 1)
	app.sync = sync.NewSync(swarm, msh, sync.BlockValidatorMock{}, conf, ch, lg.WithName("syncer"))
	ch <- 100
	if err = swarm.Start(); err != nil {
		log.Panic("error starting p2p err=%v", err)
	}

	i := 0
	for ; ; i++ {
		if lyr, err := mshDb.GetLayer(types.LayerID(i)); err != nil || lyr == nil {
			lg.Info("loaded %v layers from disk %v", i-1, err)
			break
		} else {
			lg.Info("loaded layer %v from disk ", i)
			msh.ValidateLayer(lyr)
		}
	}

	lg.Info("wait %v sec", 10)
	time.Sleep(10 * time.Second)

	app.sync.Start()
	for app.sync.VerifiedLayer() < 101 {
		lg.Info("sleep for %v sec", 20)
		time.Sleep(5 * time.Second)
		ch <- 100
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
