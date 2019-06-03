package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spf13/cobra"
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

//conf
//////////////////////////////
//todo get from configuration
var npstCfg = nipst.PostParams{
	Difficulty:           5,
	NumberOfProvenLabels: 10,
	SpaceUnit:            1024,
}

//todo get from configuration
var conf = sync.Configuration{
	Concurrency:    4,
	LayerSize:      int(100),
	RequestTimeout: 150 * time.Millisecond,
}

//////////////////////////////

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

func (app *SyncApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	lg := log.New("sync_test", "", "")
	lg.Info("------------ Start sync test -----------")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)

	if err != nil {
		panic("something got fudged while creating p2p service ")
	}

	iddbstore, err := database.NewLDBDatabase(app.Config.DataDir+"ids", 0, 0)
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	validator := nipst.NewValidator(npstCfg)
	mshDb := mesh.NewPersistentMeshDB(app.Config.DataDir, lg.WithOptions(log.Nop))
	atxdb := activation.NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(),  activation.NewIdentityStore(iddbstore), mshDb, uint64(app.Config.CONSENSUS.LayersPerEpoch), validator, lg.WithName("atxDb").WithOptions(log.Nop))
	msh := mesh.NewMesh(mshDb, atxdb, sync.ConfigTst(), &sync.MeshValidatorMock{}, &sync.MockState{}, lg.WithOptions(log.Nop))
	defer msh.Close()

	ch := make(chan types.LayerID, 1)
	app.sync = sync.NewSync(swarm, msh, sync.BlockValidatorMock{}, sync.TxValidatorMock{}, conf, ch, lg.WithName("sync"))
	ch <- 101
	if err = swarm.Start(); err != nil {
		log.Panic("error starting p2p err=%v", err)
	}

	i := 0
	for ; ; i++ {
		if lyr, err2 := msh.GetLayer(types.LayerID(i)); err2 != nil || lyr == nil {
			lg.Info("loaded %v layers from disk %v", i-1, err2)
			break
		} else {
			lg.Info("loaded layer %v from disk ", i)
			msh.ValidateLayer(lyr)
		}
	}

	lg.Info("wait %v sec", 10)
	time.Sleep(10 * time.Second)

	app.sync.Start()
	for app.sync.VerifiedLayer() < 100 {
		lg.Info("sleep for %v sec", 30)
		time.Sleep(30 * time.Second)
		ch <- 101
	}

	lg.Info("%v verified layers %v", app.BaseApp.Config.P2P.NodeID, app.sync.VerifiedLayer())
	lg.Info("sync done")
	for {
		lg.Info("keep busy sleep for %v sec", 60)
		time.Sleep(60 * time.Second)
	}
}

func main() {
	if err := Cmd.Execute(); err != nil {
		log.Info("error ", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}
