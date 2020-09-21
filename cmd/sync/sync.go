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
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spf13/cobra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Sync cmd
var cmd = &cobra.Command{
	Use:   "sync",
	Short: "start sync",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting sync")
		syncApp := newSyncApp()
		log.Info("Right after NewSyncApp %v", syncApp.Config.DataDir())
		defer syncApp.Cleanup()
		syncApp.Initialize(cmd)
		syncApp.start(cmd, args)
	},
}

//////////////////////////////

var expectedLayers int
var bucket string
var version string
var remote bool

func init() {
	//path to remote storage
	cmd.PersistentFlags().StringVarP(&bucket, "storage-path", "z", "spacemesh-sync-data", "Specify storage bucket name")

	//expected layers
	cmd.PersistentFlags().IntVar(&expectedLayers, "expected-layers", 101, "expected number of layers")

	//fetch from remote
	cmd.PersistentFlags().BoolVar(&remote, "remote-data", false, "fetch from remote")

	//request timeout
	cmd.PersistentFlags().StringVarP(&version, "version", "v", "FullBlocks/", "data version")

	cmdp.AddCommands(cmd)
}

type syncApp struct {
	*cmdp.BaseApp
	sync  *sync.Syncer
	clock *timesync.Ticker
}

func newSyncApp() *syncApp {
	return &syncApp{BaseApp: cmdp.NewBaseApp()}
}

func (app *syncApp) Cleanup() {
	err := os.RemoveAll(app.Config.DataDir())
	if err != nil {
		app.sync.Error("failed to cleanup sync: %v", err)
	}
}

func (app *syncApp) start(cmd *cobra.Command, args []string) {
	// start p2p services
	lg := log.NewDefault("sync_test")
	lg.Info("------------ Start sync test -----------")
	lg.Info("data folder: ", app.Config.DataDir())
	lg.Info("storage path: ", bucket)
	lg.Info("download from remote storage: ", remote)
	lg.Info("expected layers: ", expectedLayers)
	lg.Info("request timeout: ", app.Config.SyncRequestTimeout)
	lg.Info("data version: ", version)
	lg.Info("layers per epoch: ", app.Config.LayersPerEpoch)
	lg.Info("hdist: ", app.Config.Hdist)

	path := app.Config.DataDir()
	fullpath := filepath.Join(path, version) + "/"

	lg.Info(" anton local db path: %v layers per epoch", fullpath)

	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, lg.WithName("p2p"), app.Config.DataDir())

	if err != nil {
		panic("something got fudged while creating p2p service ")
	}

	conf := sync.Configuration{
		Concurrency:     4,
		AtxsLimit:       200,
		LayerSize:       app.Config.LayerAvgSize,
		RequestTimeout:  time.Duration(app.Config.SyncRequestTimeout) * time.Millisecond,
		SyncInterval:    2 * 60 * time.Millisecond,
		Hdist:           app.Config.Hdist,
		ValidationDelta: 30 * time.Second,
		LayersPerEpoch:  uint16(app.Config.LayersPerEpoch),
	}
	types.SetLayersPerEpoch(int32(app.Config.LayersPerEpoch))
	lg.Info("local db path: %v layers per epoch %v", path, app.Config.LayersPerEpoch)

	if remote {
		if err := getData(path, version, lg); err != nil {
			lg.Error("could not download data for test", err)
			return
		}
	}
	poetDbStore, err := database.NewLDBDatabase(fullpath+"poet", 0, 0, lg.WithName("poetDbStore"))
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	poetDb := activation.NewPoetDb(poetDbStore, lg.WithName("poetDb").WithOptions(log.Nop))

	mshdb, err := mesh.NewPersistentMeshDB(fullpath, 5, lg.WithOptions(log.Nop))
	if err != nil {
		lg.Error("error: ", err)
		return
	}
	atxdbStore, err := database.NewLDBDatabase(fullpath+"atx", 0, 0, lg)
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	txpool := state.NewTxMemPool()
	atxpool := activation.NewAtxMemPool()

	sync := sync.NewSyncWithMocks(atxdbStore, mshdb, txpool, atxpool, swarm, poetDb, conf, types.LayerID(expectedLayers))
	app.sync = sync
	if err = swarm.Start(); err != nil {
		log.Panic("error starting p2p err=%v", err)
	}

	i := conf.LayersPerEpoch * 2
	for ; ; i++ {
		log.Info("getting layer %v", i)
		if lyr, err2 := sync.GetLayer(types.LayerID(i)); err2 != nil || lyr == nil {
			l := types.LayerID(i)
			if !l.GetEpoch().IsGenesis() {
				lg.Info("loaded %v layers from disk %v", i-1, err2)
				break
			}
		} else {
			lg.Info("loaded layer %v from disk ", i)
			sync.ValidateLayer(lyr, types.BlockIDs(lyr.Blocks()))
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

//GetData downloads data from remote storage
func getData(path, prefix string, lg log.Log) error {
	fullpath := filepath.Join(path, version)
	dirs := []string{"appliedTxs", "atx", "ids", "mesh", "poet", "state", "store",
		"mesh/blocks", "mesh/general", "mesh/inputvector", "mesh/layers", "mesh/transactions",
		"mesh/unappliedTxs", "mesh/validity"}
	for _, dir := range dirs {
		dirpath := filepath.Join(fullpath, dir)
		lg.Info("Creating db folder %v", dirpath)
		if err := filesystem.ExistOrCreate(dirpath); err != nil {
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

		lg.Info("downloading: %v to %v", attrs.Name, filepath.Join(path, attrs.Name))

		err = ioutil.WriteFile(filepath.Join(path, attrs.Name), data, 0644)
		if err != nil {
			lg.Error("%v", err)
			return err
		}
		count++
	}

	lg.Info("done downloading: %v files", count)
	return nil
}

func main() {
	if err := cmd.Execute(); err != nil {
		log.Info("error ", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
