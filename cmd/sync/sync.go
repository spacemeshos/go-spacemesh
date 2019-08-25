package main

import (
	"cloud.google.com/go/storage"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
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
		defer syncApp.Cleanup()
		syncApp.Initialize(cmd)
		syncApp.Start(cmd, args)
	},
}

//////////////////////////////

var expectedLayers int
var bucket string
var remote bool
var timeout int

func init() {
	//path to remote storage
	Cmd.PersistentFlags().StringVarP(&bucket, "storage-path", "z", "spacemesh-sync-data", "Specify storage bucket name")

	//expected layers
	Cmd.PersistentFlags().IntVar(&expectedLayers, "expected-layers", 101, "expected number of layers")

	//fetch from remote
	Cmd.PersistentFlags().BoolVar(&remote, "remote-data", false, "fetch from remote")

	//request timeout
	Cmd.PersistentFlags().IntVar(&timeout, "timeout", 500, "request timeout")

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

type mockTxProcessor struct {
}

func (processor mockTxProcessor) GetValidAddressableTx(tx *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error) {
	addr, err := processor.ValidateTransactionSignature(tx)
	if err != nil {
		return nil, err
	}

	return &types.AddressableSignedTransaction{SerializableSignedTransaction: tx, Address: addr}, nil
}

func (mockTxProcessor) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error) {
	return address.HexToAddress("0xFFFF"), nil
}

type mockClock struct {
	ch    map[timesync.LayerTimer]int
	first timesync.LayerTimer
}

func (c *mockClock) Subscribe() timesync.LayerTimer {
	if c.ch == nil {
		c.ch = make(map[timesync.LayerTimer]int)
	}
	newCh := make(chan types.LayerID, 1)
	c.ch[newCh] = len(c.ch)
	if c.first == nil {
		c.first = newCh
	}

	return newCh
}

func (c *mockClock) Unsubscribe(timer timesync.LayerTimer) {
	delete(c.ch, timer)
}

func (app *SyncApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	lg := log.New("sync_test", "", "")
	lg.Info("------------ Start sync test -----------")
	lg.Info("data folder: ", app.Config.DataDir)
	lg.Info("storage path: ", bucket)
	lg.Info("download from remote storage: ", remote)
	lg.Info("expected layers: ", expectedLayers)
	lg.Info("request timeout: ", timeout)

	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)

	if err != nil {
		panic("something got fudged while creating p2p service ")
	}

	conf := sync.Configuration{
		Concurrency:    4,
		LayerSize:      int(100),
		RequestTimeout: time.Duration(timeout) * time.Millisecond,
	}

	if remote {
		if err := GetData(app.Config.DataDir, lg); err != nil {
			lg.Error("could not download data for test", err)
			return
		}
	}
	poetDbStore, err := database.NewLDBDatabase(app.Config.DataDir+"poet", 0, 0, lg.WithName("poetDbStore"))
	if err != nil {
		lg.Error("error: ", err)
		return
	}

	poetDb := activation.NewPoetDb(poetDbStore, lg.WithName("poetDb").WithOptions(log.Nop))

	mshdb, _ := mesh.NewPersistentMeshDB(app.Config.DataDir, lg.WithOptions(log.Nop))
	atxdbStore, _ := database.NewLDBDatabase(app.Config.DataDir+"atx", 0, 0, lg)
	atxdb := activation.NewActivationDb(atxdbStore, &sync.MockIStore{}, mshdb, uint16(1000), &sync.ValidatorMock{}, lg.WithName("atxDB").WithOptions(log.Nop))

	txpool := miner.NewTypesTransactionIdMemPool()
	atxpool := miner.NewTypesAtxIdMemPool()

	msh := mesh.NewMesh(mshdb, atxdb, sync.ConfigTst(), &sync.MeshValidatorMock{}, txpool, atxpool, &sync.MockState{}, lg)
	defer msh.Close()
	msh.AddBlock(&mesh.GenesisBlock)

	clock := &mockClock{}
	app.sync = sync.NewSync(swarm, msh, txpool, atxpool, mockTxProcessor{}, sync.BlockEligibilityValidatorMock{}, poetDb, conf, clock, 0, lg.WithName("sync"))
	clock.first <- types.LayerID(expectedLayers)
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
	for app.sync.ValidatedLayer() < types.LayerID(expectedLayers) {
		lg.Info("sleep for %v sec", 30)
		time.Sleep(30 * time.Second)
		clock.first <- types.LayerID(expectedLayers + 1)
	}

	lg.Info("%v verified layers %v", app.BaseApp.Config.P2P.NodeID, app.sync.ValidatedLayer())
	lg.Event().Info("sync done")
	for {
		lg.Info("keep busy sleep for %v sec", 60)
		time.Sleep(60 * time.Second)
	}
}

//download data from remote storage
func GetData(path string, lg log.Log) error {
	dirs := []string{"poet", "atx", "nipst", "blocks", "ids", "layers", "transactions", "validity"}
	for _, dir := range dirs {
		if err := os.MkdirAll(path+dir, 0777); err != nil {
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
	it := client.Bucket(bucket).Objects(ctx, nil)
	count := 0
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}

		lg.Info("downloading:", attrs.Name)
		rc, err := client.Bucket(bucket).Object(attrs.Name).NewReader(ctx)
		if err != nil {
			return err
		}

		defer rc.Close()

		data, err := ioutil.ReadAll(rc)
		if err != nil {
			return err
		}

		err = ioutil.WriteFile("bin/"+attrs.Name, data, 0644)
		if err != nil {
			return err
		}
		count++
	}

	lg.Info("downloaded: %v files ", count)
	return nil
}

func main() {
	if err := Cmd.Execute(); err != nil {
		log.Info("error ", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
