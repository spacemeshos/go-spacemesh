package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	SchemaVersion = "https://spacemesh.io/bootstrap.schema.json.1.0"
	confirmation  = 7
	timeout       = 5 * time.Second
)

func PersistedFilename(epoch types.EpochID, suffix string) string {
	return filepath.Join(dataDir, bootstrap.UpdateName(epoch, suffix))
}

type Generator struct {
	logger      *zap.Logger
	fs          afero.Fs
	client      *retryablehttp.Client
	btcEndpoint string
	smEndpoint  string
}

type Opt func(*Generator)

func WithLogger(logger *zap.Logger) Opt {
	return func(g *Generator) {
		g.logger = logger
	}
}

func WithFilesystem(fs afero.Fs) Opt {
	return func(g *Generator) {
		g.fs = fs
	}
}

func NewGenerator(btcEndpoint, smEndpoint string, opts ...Opt) *Generator {
	client := retryablehttp.NewClient()
	client.RetryWaitMin = 500 * time.Millisecond
	client.RetryWaitMax = time.Second
	client.Backoff = retryablehttp.LinearJitterBackoff

	g := &Generator{
		logger:      zap.NewNop(),
		fs:          afero.NewOsFs(),
		client:      client,
		btcEndpoint: btcEndpoint,
		smEndpoint:  smEndpoint,
	}

	for _, opt := range opts {
		opt(g)
	}

	return g
}

func (g *Generator) SmEndpoint() string {
	return g.smEndpoint
}

func (g *Generator) Generate(
	ctx context.Context,
	targetEpoch types.EpochID,
	genBeacon, genActiveSet bool,
) (string, error) {
	if err := g.fs.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create persist dir %v: %w", dataDir, err)
	}
	var (
		suffix    string
		beacon    types.Beacon
		activeSet []types.ATXID
		err       error
	)
	if genBeacon && genActiveSet {
		suffix = bootstrap.SuffixBootstrap
	} else if genBeacon {
		suffix = bootstrap.SuffixBeacon
	} else if genActiveSet {
		suffix = bootstrap.SuffixActiveSet
	} else {
		g.logger.Fatal("nothing to do")
	}

	if genBeacon {
		beacon, err = g.genBeacon(ctx, g.logger)
		if err != nil {
			return "", err
		}
	}
	if genActiveSet {
		activeSet, err = getActiveSet(ctx, g.smEndpoint, targetEpoch-1)
		if err != nil {
			return "", err
		}
	}
	return g.GenUpdate(targetEpoch, beacon, activeSet, suffix)
}

// BitcoinResponse captures the only fields we care about from a bitcoin block.
type BitcoinResponse struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
}

func (g *Generator) genBeacon(ctx context.Context, logger *zap.Logger) (types.Beacon, error) {
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

func bitcoinHash(
	ctx context.Context,
	logger *zap.Logger,
	client *retryablehttp.Client,
	targetUrl string,
) (*BitcoinResponse, error) {
	latest, err := queryBitcoin(ctx, client, targetUrl)
	if err != nil {
		return nil, err
	}
	logger.Info("latest bitcoin block height", zap.Uint64("height", latest.Height), zap.String("hash", latest.Hash))
	height := latest.Height - confirmation

	blockUrl := fmt.Sprintf("%s/blocks/%d", targetUrl, height)
	confirmed, err := queryBitcoin(ctx, client, blockUrl)
	if err != nil {
		return nil, err
	}
	logger.Info("confirmed bitcoin block", zap.Uint64("height", confirmed.Height), zap.String("hash", confirmed.Hash))
	return confirmed, nil
}

func queryBitcoin(ctx context.Context, client *retryablehttp.Client, targetUrl string) (*BitcoinResponse, error) {
	resource, err := url.Parse(targetUrl)
	if err != nil {
		return nil, fmt.Errorf("parse btc url: %w", err)
	}
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, resource.String(), nil)
	// some apis got over sensitive without UA set
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:125.0) Gecko/20100101 Firefox/125.0")
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get latest bitcoin block: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching bitcoin block unsuccessful: %s", resp.Status)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bootstrap read response: %w", err)
	}
	var br BitcoinResponse
	err = json.Unmarshal(data, &br)
	if err != nil {
		return nil, fmt.Errorf("unmarshal bitcoin response: %w", err)
	}
	return &br, nil
}

func getActiveSet(ctx context.Context, endpoint string, epoch types.EpochID) ([]types.ATXID, error) {
	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %v: %w", endpoint, err)
	}
	defer conn.Close()

	client := pb.NewMeshServiceClient(conn)
	stream, err := client.EpochStream(ctx, &pb.EpochStreamRequest{Epoch: uint32(epoch)})
	if err != nil {
		return nil, fmt.Errorf("epoch stream %v: %w", endpoint, err)
	}
	activeSet := make([]types.ATXID, 0, 300_000)
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		activeSet = append(activeSet, types.ATXID(types.BytesToHash(resp.GetId().GetId())))
	}
	sort.Slice(activeSet, func(i, j int) bool {
		return bytes.Compare(activeSet[i].Bytes(), activeSet[j].Bytes()) < 0
	})
	return activeSet, nil
}

func (g *Generator) GenUpdate(
	epoch types.EpochID,
	beacon types.Beacon,
	activeSet []types.ATXID,
	suffix string,
) (string, error) {
	as := make([]string, 0, len(activeSet))
	for _, atx := range activeSet {
		as = append(as, hex.EncodeToString(atx.Bytes())) // no leading 0x
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
		Epoch: edata,
	}
	data, err := json.Marshal(update)
	if err != nil {
		return "", fmt.Errorf("marshal data: %w", err)
	}
	// make sure the data is valid
	if err = bootstrap.ValidateSchema(data); err != nil {
		return "", fmt.Errorf("invalid data: %w", err)
	}
	filename := PersistedFilename(epoch, suffix)
	err = afero.WriteFile(g.fs, filename, data, 0o600)
	if err != nil {
		return "", fmt.Errorf("persist epoch update %v: %w", filename, err)
	}
	g.logger.Info("generated update",
		zap.String("filename", filename),
	)
	return filename, nil
}
