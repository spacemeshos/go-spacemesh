package main

import (
	"cloud.google.com/go/storage"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spf13/cobra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"io/ioutil"
	"net/http"
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
		log.Info("Right after NewSyncApp %v", syncApp.Config.DataDir)
		defer syncApp.Cleanup()
		syncApp.Initialize(cmd)
		syncApp.Start(cmd, args)
	},
}

//////////////////////////////

var expectedLayers int
var bucket string
var version string
var remote bool

func init() {
	//path to remote storage
	Cmd.PersistentFlags().StringVarP(&bucket, "storage-path", "z", "spacemesh-sync-data", "Specify storage bucket name")

	//expected layers
	Cmd.PersistentFlags().IntVar(&expectedLayers, "expected-layers", 101, "expected number of layers")

	//fetch from remote
	Cmd.PersistentFlags().BoolVar(&remote, "remote-data", false, "fetch from remote")

	//request timeout
	Cmd.PersistentFlags().StringVarP(&version, "version", "v", "FullBlocks/", "data version")

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
	err := os.RemoveAll(app.Config.DataDir)
	if err != nil {
		app.sync.Error("failed to cleanup sync: %v", err)
	}
}

type MockBlockBuilder struct {
	txs []*types.Transaction
}

func (m *MockBlockBuilder) ValidateAndAddTxToPool(tx *types.Transaction) error {
	m.txs = append(m.txs, tx)
	return nil
}

func (app *SyncApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	lg := log.New("sync_test", "", "")
	lg.Info("------------ Start sync test -----------")
	lg.Info("data folder: ", app.Config.DataDir)
	lg.Info("storage path: ", bucket)
	lg.Info("download from remote storage: ", remote)
	lg.Info("expected layers: ", expectedLayers)
	lg.Info("request timeout: ", app.Config.SyncRequestTimeout)
	lg.Info("data version: ", version)
	lg.Info("layers per epoch: ", app.Config.LayersPerEpoch)
	lg.Info("hdist: ", app.Config.Hdist)

	path := app.Config.DataDir + version

	lg.Info("local db path: ", path)

	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, lg.WithName("p2p"), app.Config.DataDir)

	if err != nil {
		panic("something got fudged while creating p2p service ")
	}

	conf := sync.Configuration{
		Concurrency:     4,
		AtxsLimit:       200,
		LayerSize:       int(app.Config.LayerAvgSize),
		RequestTimeout:  time.Duration(app.Config.SyncRequestTimeout) * time.Millisecond,
		SyncInterval:    2 * 60 * time.Millisecond,
		Hdist:           app.Config.Hdist,
		ValidationDelta: 30 * time.Second,
	}

	if remote {
		if err := GetData(app.Config.DataDir, version, lg); err != nil {
			lg.Error("could not download data for test", err)
			return
		}
	}
	poetDbStore, err := database.NewLDBDatabase(path+"poet", 0, 0, lg.WithName("poetDbStore"))
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	poetDb := activation.NewPoetDb(poetDbStore, lg.WithName("poetDb").WithOptions(log.Nop))

	mshdb, err := mesh.NewPersistentMeshDB(path, 5, lg.WithOptions(log.Nop))
	if err != nil {
		lg.Error("error: ", err)
		return
	}
	atxdbStore, err := database.NewLDBDatabase(path+"atx", 0, 0, lg)
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	atxdb := activation.NewActivationDb(atxdbStore, &sync.MockIStore{}, mshdb, uint16(app.Config.LayersPerEpoch), &sync.ValidatorMock{}, lg.WithName("atxDB").WithOptions(log.Nop))

	txpool := miner.NewTxMemPool()
	atxpool := miner.NewAtxMemPool()

	var msh *mesh.Mesh
	if mshdb.PersistentData() {
		lg.Info("persistent data found ")
		msh = mesh.NewRecoveredMesh(mshdb, atxdb, sync.ConfigTst(), &sync.MeshValidatorMock{}, txpool, atxpool, &sync.MockState{}, lg)
	} else {
		lg.Info("no persistent data found ")
		msh = mesh.NewMesh(mshdb, atxdb, sync.ConfigTst(), &sync.MeshValidatorMock{}, txpool, atxpool, &sync.MockState{}, lg)
	}

	msh.SetBlockBuilder(&MockBlockBuilder{})

	defer msh.Close()
	msh.AddBlock(mesh.GenesisBlock)
	clock := sync.MockClock{}
	clock.Layer = types.LayerID(expectedLayers + 1)
	lg.Info("current layer %v", clock.GetCurrentLayer())
	app.sync = sync.NewSync(swarm, msh, txpool, atxpool, sync.BlockEligibilityValidatorMock{}, poetDb, conf, &clock, lg.WithName("sync"))
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

	sleep := time.Duration(10) * time.Second
	lg.Info("wait %v sec", sleep)
	app.sync.Start()
	for app.sync.ProcessedLayer() < types.LayerID(expectedLayers) {
		app.sync.ForceSync()
		lg.Info("sleep for %v sec", 30)
		time.Sleep(30 * time.Second)

	}

	lg.Info("%v verified layers %v", app.BaseApp.Config.P2P.NodeID, app.sync.ProcessedLayer())
	lg.Event().Info("sync done")
	for {
		lg.Info("keep busy sleep for %v sec", 60)
		time.Sleep(60 * time.Second)
	}
}

//download data from remote storage
func GetData(path, prefix string, lg log.Log) error {
	dirs := []string{"poet", "atx", "nipst", "blocks", "ids", "layers", "transactions", "validity", "unappliedTxs"}
	for _, dir := range dirs {
		if err := filesystem.ExistOrCreate(path + prefix + "/" + dir); err != nil {
			return err
		}
	}

	c := http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS11,
				InsecureSkipVerify: true,
			},
		},
		Timeout: 2 * time.Second,
	}

	ctx := context.TODO()
	client, err := storage.NewClient(ctx, option.WithoutAuthentication(), option.WithHTTPClient(&c))
	if err != nil {
		panic(err)
	}
	it := client.Bucket(bucket).Objects(ctx, &storage.Query{
		Prefix: prefix,
	})

	count := 0
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}

		rc, err := client.Bucket(bucket).Object(attrs.Name).NewReader(ctx)
		if err != nil {
			return err
		}

		defer rc.Close()

		data, err := ioutil.ReadAll(rc)
		if err != nil {
			return err
		}

		//skip main folder
		if attrs.Name == version {
			continue
		}

		lg.Info("downloading: %v to %v", attrs.Name, path+attrs.Name)

		err = ioutil.WriteFile(path+attrs.Name, data, 0644)
		if err != nil {
			lg.Info("%v", err)
			return err
		}
		count++
	}

	lg.Info("Done downloading: %v files", count)
	return nil
}

func main() {
	if err := Cmd.Execute(); err != nil {
		log.Info("error ", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
