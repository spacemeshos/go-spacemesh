package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
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

func persistDir() string {
	return filepath.Clean(dataDir)
}

func epochFile(epoch types.EpochID) string {
	return filepath.Join(persistDir(), fmt.Sprintf("epoch-update-%05d", epoch))
}

type Generator struct {
	logger       log.Log
	clock        LayerClock
	fs           afero.Fs
	client       *http.Client
	offset       uint32
	bitcoinUrl   string
	nodeEndpoint string
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

func NewGenerator(clock LayerClock, offset uint32, btcUrl string, endpoint string, opts ...Opt) *Generator {
	g := &Generator{
		logger:       log.NewNop(),
		fs:           afero.NewOsFs(),
		client:       &http.Client{},
		clock:        clock,
		offset:       offset,
		bitcoinUrl:   btcUrl,
		nodeEndpoint: endpoint,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *Generator) Run(ctx context.Context) error {
	if err := g.fs.MkdirAll(persistDir(), 0o700); err != nil {
		return err
	}

	current := g.clock.CurrentLayer().GetEpoch()
	g.generateIfAbsent(ctx, current)

	from := current
	if from == 0 {
		from = types.EpochID(1)
	}
	for epoch := from; ; epoch = epoch + 1 {
		updateLayer := (epoch + 1).FirstLayer().Sub(g.offset)
		g.logger.With().Info("waiting for layer", updateLayer)
		select {
		case <-g.clock.AwaitLayer(updateLayer):
			g.generateIfAbsent(ctx, epoch+1)
		case <-ctx.Done():
			return nil
		}
	}
}

func (g *Generator) generateIfAbsent(ctx context.Context, targetEpoch types.EpochID) {
	if targetEpoch.IsGenesis() {
		return
	}

	logger := g.logger.WithContext(ctx).WithFields(g.clock.CurrentLayer(), log.Stringer("target_epoch", targetEpoch))
	filename := epochFile(targetEpoch)
	exists, err := afero.Exists(g.fs, filename)
	if err != nil {
		logger.With().Error("failed to stat epoch update",
			log.String("filename", filename),
			log.Err(err),
		)
		return
	}
	if exists {
		return
	}
	logger.Info("epoch update not yet generated")
	if err = g.generate(ctx, logger, targetEpoch); err != nil {
		logger.With().Error("failed to generate epoch update", log.Err(err))
	}
}

func (g *Generator) generate(ctx context.Context, logger log.Log, epoch types.EpochID) error {
	beacon, err := g.genBeacon(ctx, logger)
	if err != nil {
		return err
	}
	activeSet, err := getActiveSet(ctx, logger, g.nodeEndpoint, epoch-1)
	if err != nil {
		return err
	}
	_, err = genUpdate(logger, g.fs, epoch, beacon, activeSet)
	return err
}

// BitcoinResponse captures the only fields we care about from a bitcoin block.
type BitcoinResponse struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
}

func (g *Generator) genBeacon(ctx context.Context, logger log.Log) (types.Beacon, error) {
	if g.bitcoinUrl == "" {
		b := make([]byte, types.BeaconSize)
		_, err := rand.Read(b)
		if err != nil {
			return types.EmptyBeacon, err
		}
		return types.BytesToBeacon(b), nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	br, err := bitcoinHash(ctx, logger, g.client, g.bitcoinUrl)
	if err != nil {
		return types.EmptyBeacon, err
	}
	decoded, err := hex.DecodeString(br.Hash)
	if err != nil {
		return types.EmptyBeacon, fmt.Errorf("decode bitcoin hash: %w", err)
	}
	offset := len(decoded) - types.BeaconSize
	beacon := types.BytesToBeacon(decoded[offset:])
	return beacon, nil
}

func bitcoinHash(ctx context.Context, logger log.Log, client *http.Client, targetUrl string) (*BitcoinResponse, error) {
	latest, err := queryBitcoin(ctx, logger, client, targetUrl)
	if err != nil {
		return nil, err
	}
	logger.With().Info("latest bitcoin block height",
		log.Uint64("height", latest.Height),
		log.String("hash", latest.Hash),
	)
	height := latest.Height - confirmation

	blockUrl := fmt.Sprintf("%s/blocks/%d", targetUrl, height)
	confirmed, err := queryBitcoin(ctx, logger, client, blockUrl)
	if err != nil {
		return nil, err
	}
	logger.With().Info("confirmed bitcoin block",
		log.Uint64("height", confirmed.Height),
		log.String("hash", confirmed.Hash),
	)
	return confirmed, nil
}

func queryBitcoin(ctx context.Context, logger log.Log, client *http.Client, targetUrl string) (*BitcoinResponse, error) {
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
	logger.With().Debug("bitcoin block", log.String("content", string(data)))
	var br BitcoinResponse
	err = json.Unmarshal(data, &br)
	if err != nil {
		return nil, fmt.Errorf("unmarshal bitcoin response: %w", err)
	}
	return &br, nil
}

func getActiveSet(ctx context.Context, logger log.Log, endpoint string, epoch types.EpochID) ([]types.ATXID, error) {
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
	logger.With().Info("received active set", log.Int("size", len(activeSet)))
	return activeSet, nil
}

func genUpdate(
	logger log.Log,
	fs afero.Fs,
	epoch types.EpochID,
	beacon types.Beacon,
	activeSet []types.ATXID,
) (string, error) {
	as := make([]string, 0, len(activeSet))
	for _, atx := range activeSet {
		as = append(as, hex.EncodeToString(atx.Hash32().Bytes())) // no leading 0x
	}
	var update bootstrap.Update
	update.Version = SchemaVersion
	update.Data = bootstrap.InnerData{
		ID: uint32(epoch),
		Epochs: []bootstrap.EpochData{
			{
				Epoch:     uint32(epoch),
				Beacon:    hex.EncodeToString(beacon.Bytes()), // no leading 0x
				ActiveSet: as,
			},
		},
	}
	data, err := json.Marshal(update)
	if err != nil {
		return "", fmt.Errorf("marshal data %v: %w", string(data), err)
	}
	// make sure the data is valid
	if err = bootstrap.ValidateSchema(data); err != nil {
		return "", fmt.Errorf("invalid data %v: %w", string(data), err)
	}
	filename := epochFile(epoch)
	err = afero.WriteFile(fs, filename, data, 0o400)
	if err != nil {
		return "", fmt.Errorf("persist epoch update %v: %w", filename, err)
	}
	logger.With().Info("generated update",
		log.String("update", string(data)),
		log.String("filename", filename),
	)
	return filename, nil
}
