package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	SchemaVersion = "https://spacemesh.io/bootstrap.schema.json.1.0"
	confirmation  = 7
	timeout       = 5 * time.Second
)

func PersistedFilename() string {
	return filepath.Join(dataDir, "spacemesh-update")
}

type Generator struct {
	logger      log.Log
	fs          afero.Fs
	client      *http.Client
	btcEndpoint string
	smEndpoint  string
}

type Opt func(*Generator)

func WithLogger(logger log.Log) Opt {
	return func(g *Generator) {
		g.logger = logger
	}
}

func WithFilesystem(fs afero.Fs) Opt {
	return func(g *Generator) {
		g.fs = fs
	}
}

func WithHttpClient(c *http.Client) Opt {
	return func(g *Generator) {
		g.client = c
	}
}

func NewGenerator(btcEndpoint string, smEndpoint string, opts ...Opt) *Generator {
	g := &Generator{
		logger:      log.NewNop(),
		fs:          afero.NewOsFs(),
		client:      &http.Client{},
		btcEndpoint: btcEndpoint,
		smEndpoint:  smEndpoint,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *Generator) Generate(ctx context.Context, targetEpoch types.EpochID, genBeacon, genActiveSet bool) error {
	if err := g.fs.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create persist dir %v: %w", dataDir, err)
	}
	var (
		beacon    types.Beacon
		activeSet []types.ATXID
		err       error
	)
	if genBeacon {
		beacon, err = g.genBeacon(ctx, g.logger, targetEpoch)
		if err != nil {
			return err
		}
	}
	if genActiveSet {
		activeSet, err = getActiveSet(ctx, g.smEndpoint, targetEpoch-1)
		if err != nil {
			return err
		}
	}
	return g.genUpdate(targetEpoch, beacon, activeSet)
}

func (g *Generator) GenBootstrap(ctx context.Context, epoch types.EpochID) error {
	return g.Generate(ctx, epoch, true, true)
}

func (g *Generator) GenFallbackBeacon(ctx context.Context, epoch types.EpochID) error {
	return g.Generate(ctx, epoch, true, false)
}

func (g *Generator) GenFallbackActiveSet(ctx context.Context, epoch types.EpochID) error {
	return g.Generate(ctx, epoch, false, true)
}

// BitcoinResponse captures the only fields we care about from a bitcoin block.
type BitcoinResponse struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
}

func (g *Generator) genBeacon(ctx context.Context, logger log.Log, epoch types.EpochID) (types.Beacon, error) {
	if g.btcEndpoint == "" {
		b := make([]byte, types.BeaconSize)
		binary.LittleEndian.PutUint32(b, uint32(epoch))
		return types.BytesToBeacon(b), nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	br, err := bitcoinHash(ctx, logger, g.client, g.btcEndpoint)
	if err != nil {
		return types.EmptyBeacon, err
	}
	decoded, err := hex.DecodeString(br.Hash)
	if err != nil {
		return types.EmptyBeacon, fmt.Errorf("decode bitcoin hash: %w", err)
	}
	// bitcoin hash started with leading zero. we want to grab 4 LSB
	offset := len(decoded) - types.BeaconSize
	beacon := types.BytesToBeacon(decoded[offset:])
	return beacon, nil
}

func bitcoinHash(ctx context.Context, logger log.Log, client *http.Client, targetUrl string) (*BitcoinResponse, error) {
	latest, err := queryBitcoin(ctx, client, targetUrl)
	if err != nil {
		return nil, err
	}
	logger.With().Info("latest bitcoin block height",
		log.Uint64("height", latest.Height),
		log.String("hash", latest.Hash),
	)
	height := latest.Height - confirmation

	blockUrl := fmt.Sprintf("%s/blocks/%d", targetUrl, height)
	confirmed, err := queryBitcoin(ctx, client, blockUrl)
	if err != nil {
		return nil, err
	}
	logger.With().Info("confirmed bitcoin block",
		log.Uint64("height", confirmed.Height),
		log.String("hash", confirmed.Hash),
	)
	return confirmed, nil
}

func queryBitcoin(ctx context.Context, client *http.Client, targetUrl string) (*BitcoinResponse, error) {
	resource, err := url.Parse(targetUrl)
	if err != nil {
		return nil, fmt.Errorf("parse btc url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get latest bitcoin block: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bootstrap read resonse: %w", err)
	}
	var br BitcoinResponse
	err = json.Unmarshal(data, &br)
	if err != nil {
		return nil, fmt.Errorf("unmarshal bitcoin response: %w", err)
	}
	return &br, nil
}

func getActiveSet(ctx context.Context, endpoint string, epoch types.EpochID) ([]types.ATXID, error) {
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %v: %w", endpoint, err)
	}

	client := pb.NewMeshServiceClient(conn)
	stream, err := client.EpochStream(ctx, &pb.EpochStreamRequest{Epoch: uint32(epoch)})
	if err != nil {
		return nil, fmt.Errorf("epoch stream %v: %w", endpoint, err)
	}
	activeSet := make([]types.ATXID, 0, 10_000)
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		activeSet = append(activeSet, types.ATXID(types.BytesToHash(resp.GetId().GetId())))
	}
	return activeSet, nil
}

func (g *Generator) genUpdate(epoch types.EpochID, beacon types.Beacon, activeSet []types.ATXID) error {
	as := make([]string, 0, len(activeSet))
	for _, atx := range activeSet {
		as = append(as, hex.EncodeToString(atx.Hash32().Bytes())) // no leading 0x
	}
	var update bootstrap.Update
	update.Version = SchemaVersion
	edata := bootstrap.EpochData{
		ID:     uint32(epoch),
		Beacon: hex.EncodeToString(beacon.Bytes()), // no leading 0x
	}
	if len(activeSet) > 0 {
		edata.ActiveSet = as
	}
	update.Data = bootstrap.InnerData{
		UpdateId: time.Now().Unix(),
		Epoch:    edata,
	}
	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshal data %v: %w", string(data), err)
	}
	// make sure the data is valid
	if err = bootstrap.ValidateSchema(data); err != nil {
		return fmt.Errorf("invalid data %v: %w", string(data), err)
	}
	filename := PersistedFilename()
	err = afero.WriteFile(g.fs, filename, data, 0o600)
	if err != nil {
		return fmt.Errorf("persist epoch update %v: %w", filename, err)
	}
	g.logger.With().Info("generated update",
		log.String("update", string(data)),
		log.String("filename", filename),
	)
	return nil
}
