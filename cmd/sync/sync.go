package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"cloud.google.com/go/storage"
	"github.com/spf13/cobra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

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
	"strings"
)

// Sync cmd
var cmd = &cobra.Command{
	Use:   "sync",
	Short: "start sync",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("starting sync")
		syncApp := newSyncApp()
		log.With().Info("initializing new sync app", log.String("DataDir", syncApp.Config.DataDir()))
		defer syncApp.Cleanup()
		syncApp.Initialize(cmd)
		syncApp.start(cmd, args)
	},
}

// ////////////////////////////

var expectedLayers int
var bucket string
var version string
var remote bool

func init() {
	// path to remote storage
	cmd.PersistentFlags().StringVarP(&bucket, "storage-path", "z", "spacemesh-sync-data", "Specify storage bucket name")

	// expected layers
	cmd.PersistentFlags().IntVar(&expectedLayers, "expected-layers", 101, "expected number of layers")

	// fetch from remote
	cmd.PersistentFlags().BoolVar(&remote, "remote-data", false, "fetch from remote")

	// request timeout
	cmd.PersistentFlags().StringVarP(&version, "version", "v", "samples/", "data version")

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
		app.sync.With().Error("failed to cleanup sync", log.Err(err))
	}
}

func (app *syncApp) start(cmd *cobra.Command, args []string) {
	// start p2p services
	lg := log.NewDefault("sync_test")
	lg.With().Info("------------ Start sync test -----------",
		log.String("data_folder", app.Config.DataDir()),
		log.String("storage_path", bucket),
		log.Bool("download_from_remote_storage", remote),
		log.Int("expected_layers", expectedLayers),
		log.Int("request_timeout", app.Config.SyncRequestTimeout),
		log.String("data_version", version),
		log.Int("layers_per_epoch", app.Config.LayersPerEpoch),
		log.Int("hdist", app.Config.Hdist),
	)

	path := app.Config.DataDir()
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, lg.WithName("p2p"), app.Config.DataDir())

	if err != nil {
		panic("something got fudged while creating p2p service ")
	}

	goldenATXID := types.ATXID(types.HexToHash32(app.Config.GoldenATXID))

	conf := sync.Configuration{
		Concurrency:     4,
		AtxsLimit:       200,
		LayerSize:       app.Config.LayerAvgSize,
		RequestTimeout:  time.Duration(app.Config.SyncRequestTimeout) * time.Millisecond,
		SyncInterval:    2 * 60 * time.Millisecond,
		Hdist:           app.Config.Hdist,
		ValidationDelta: 30 * time.Second,
		LayersPerEpoch:  uint16(app.Config.LayersPerEpoch),
		GoldenATXID:     goldenATXID,
	}
	types.SetLayersPerEpoch(int32(app.Config.LayersPerEpoch))
	lg.Info("local db path: %v layers per epoch: %v", path, app.Config.LayersPerEpoch)

	if remote {
		if err := getData(path, version, lg); err != nil {
			lg.With().Error("could not download data for test", log.Err(err))
			return
		}
	}
	poetDbStore, err := database.NewLDBDatabase(filepath.Join(path, "poet"), 0, 0, lg.WithName("poetDbStore"))
	if err != nil {
		lg.With().Error("error creating poet database", log.Err(err))
		return
	}

	poetDb := activation.NewPoetDb(poetDbStore, lg.WithName("poetDb").WithOptions(log.Nop))

	mshdb, err := mesh.NewPersistentMeshDB(filepath.Join(path, "mesh"), 5, lg.WithOptions(log.Nop))
	if err != nil {
		lg.With().Error("error creating mesh database", log.Err(err))
		return
	}
	atxdbStore, err := database.NewLDBDatabase(filepath.Join(path, "atx"), 0, 0, lg)
	if err != nil {
		lg.With().Error("error creating atx database", log.Err(err))
		return
	}

	txpool := state.NewTxMemPool()
	atxpool := activation.NewAtxMemPool()

	app.sync = sync.NewSyncWithMocks(atxdbStore, mshdb, txpool, atxpool, swarm, poetDb, conf, goldenATXID, types.LayerID(expectedLayers), poetDbStore)
	if err = swarm.Start(cmdp.Ctx); err != nil {
		log.With().Panic("error starting p2p", log.Err(err))
	}

	i := conf.LayersPerEpoch * 2
	for ; ; i++ {
		lg.With().Info("getting layer", types.LayerID(i))
		if lyr, err2 := app.sync.GetLayer(types.LayerID(i)); err2 != nil || lyr == nil {
			l := types.LayerID(i)
			if l > types.GetEffectiveGenesis() {
				lg.With().Info("finished loading layers from disk",
					log.FieldNamed("layers_loaded", types.LayerID(i-1)),
					log.Err(err2),
				)
				break
			}
		} else {
			lg.With().Info("loaded layer from disk", types.LayerID(i))
			app.sync.ValidateLayer(lyr)
		}
	}

	sleep := time.Duration(10) * time.Second
	lg.Info("wait %v sec", sleep)
	app.sync.Start(cmdp.Ctx)
	for app.sync.ProcessedLayer() < types.LayerID(expectedLayers) {
		app.sync.ForceSync()
		lg.Info("sleep for %v sec", 30)
		time.Sleep(30 * time.Second)

	}

	lg.Event().Info("sync done",
		log.String("node_id", app.BaseApp.Config.P2P.NodeID),
		log.FieldNamed("verified_layers", app.sync.ProcessedLayer()),
	)
	for {
		lg.Info("keep busy sleep for %v sec", 60)
		time.Sleep(60 * time.Second)
	}
}

// GetData downloads data from remote storage
func getData(path, prefix string, lg log.Log) error {
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

		data, err := ioutil.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return err
		}

		// skip main folder
		if attrs.Name == version {
			continue
		}
		dest := path + strings.TrimPrefix(attrs.Name, version)
		if err := ensureDirExists(dest); err != nil {
			return err
		}
		lg.Info("downloading: %v to %v", attrs.Name, dest)

		err = ioutil.WriteFile(dest, data, 0644)
		if err != nil {
			lg.Error("%v", err)
			return err
		}
		count++
	}

	lg.Info("done downloading: %v files", count)
	return nil
}

func ensureDirExists(path string) error {
	dir, _ := filepath.Split(path)
	return filesystem.ExistOrCreate(dir)
}

func main() {
	if err := cmd.Execute(); err != nil {
		log.With().Info("error", log.Err(err))
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
