// Package bootstrap checks for the bootstrap/fallback data update from the
// spacemesh administrator (a centralized entity controlled by the spacemesh
// team). This is intended as a short-term solution at the beginning of the
// network deployment to facilitate recovering from network failures and
// should be removed once the network is stable.
//
// The updater periodically checks for the latest update from a URL provided
// by the spacemesh administrator, verifies the data, persists on disk and
// notifies subscribers of a new update.
//
// Subscribers register by calling `Subscribe()` to receive a channel for
// the latest update.
package bootstrap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	DefaultURI = "http://localhost:3000/bootstrap"
	DirName    = "bootstrap"

	httpTimeout   = 5 * time.Second
	notifyTimeout = time.Second
	schemaFile    = "schema.json"
	format        = "2006-01-02T15-04-05"
)

var (
	ErrEpochOutOfOrder = errors.New("epoch out of order")
	ErrWrongVersion    = errors.New("wrong schema version")
	ErrInvalidBeacon   = errors.New("invalid beacon")
)

type client struct{}

func (client) Query(ctx context.Context, resource *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", resource.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get bootstrap file: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bootstrap read resonse: %w", err)
	}
	return data, nil
}

type Config struct {
	URI     string `mapstructure:"bootstrap-uri"`
	Version string `mapstructure:"bootstrap-version"`

	DataDir   string
	Interval  time.Duration
	NumToKeep int
}

func DefaultConfig() Config {
	return Config{
		URI:       DefaultURI,
		Version:   "https://spacemesh.io/bootstrap.schema.json.1.0",
		DataDir:   os.TempDir(),
		Interval:  30 * time.Second,
		NumToKeep: 10,
	}
}

type Updater struct {
	cfg    Config
	logger log.Log
	fs     afero.Fs
	client httpclient
	once   sync.Once
	eg     errgroup.Group

	mu          sync.Mutex
	subscribers []chan *VerifiedUpdate
	latest      *VerifiedUpdate
}

type Opt func(*Updater)

func WithConfig(cfg Config) Opt {
	return func(u *Updater) {
		u.cfg = cfg
	}
}

func WithLogger(logger log.Log) Opt {
	return func(u *Updater) {
		u.logger = logger
	}
}

func WithFilesystem(fs afero.Fs) Opt {
	return func(u *Updater) {
		u.fs = fs
	}
}

func WithHttpclient(c httpclient) Opt {
	return func(u *Updater) {
		u.client = c
	}
}

func New(opts ...Opt) *Updater {
	u := &Updater{
		cfg:    DefaultConfig(),
		logger: log.NewNop(),
		fs:     afero.NewOsFs(),
		client: client{},
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

func (u *Updater) Subscribe() chan *VerifiedUpdate {
	u.mu.Lock()
	defer u.mu.Unlock()
	ch := make(chan *VerifiedUpdate, 10)
	u.subscribers = append(u.subscribers, ch)
	return ch
}

func (u *Updater) Load(ctx context.Context) error {
	verified, err := load(u.fs, u.cfg)
	if err != nil {
		return err
	}
	if verified != nil {
		if err = u.updateAndNotify(ctx, verified); err != nil {
			return err
		}
	}
	return nil
}

func (u *Updater) Start(ctx context.Context) {
	u.once.Do(func() {
		u.eg.Go(func() error {
			if err := u.Load(ctx); err != nil {
				return err
			}
			wait := time.Duration(0)
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
					ctx := log.WithNewSessionID(ctx)
					if err := u.DoIt(ctx); err != nil {
						u.logger.With().Error("failed to get bootstrap update", log.Err(err))
					}
				}
				wait = u.cfg.Interval
			}
		})
	})
}

func (u *Updater) Close() {
	u.eg.Wait()
}

func (u *Updater) latestUpdateId() uint32 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.latest != nil {
		return u.latest.ID
	}
	return 0
}

func (u *Updater) DoIt(ctx context.Context) error {
	logger := u.logger.WithContext(ctx)
	verified, data, err := get(u.client, u.cfg, u.latestUpdateId())
	if err != nil {
		return err
	}
	if verified == nil { // no new update
		return nil
	}
	verified.Persisted, err = persist(logger, u.fs, u.cfg, verified.ID, data)
	if err != nil {
		return err
	}
	logger.With().Info("new bootstrap file", log.Inline(verified))
	if err = u.updateAndNotify(ctx, verified); err != nil {
		return err
	}
	return nil
}

func (u *Updater) updateAndNotify(ctx context.Context, verified *VerifiedUpdate) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.latest = verified
	notifyCtx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	for _, ch := range u.subscribers {
		select {
		case ch <- verified:
		case <-notifyCtx.Done():
			return fmt.Errorf("notify subscriber: %w", notifyCtx.Err())
		}
	}
	return nil
}

func get(client httpclient, cfg Config, lastUpdate uint32) (*VerifiedUpdate, []byte, error) {
	var (
		data     []byte
		err      error
		resource *url.URL
	)
	resource, err = url.Parse(cfg.URI)
	if err != nil {
		return nil, nil, fmt.Errorf("parse bootstrap uri: %w", err)
	}
	if resource.Scheme == "https" || resource.Scheme == "http" {
		ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
		defer cancel()
		data, err = client.Query(ctx, resource)
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("scheme not supported %v", resource.Scheme)
	}

	verified, err := validate(cfg, resource.String(), data, lastUpdate)
	if err != nil {
		return nil, nil, err
	}
	return verified, data, nil
}

func validate(cfg Config, source string, data []byte, lastUpdate uint32) (*VerifiedUpdate, error) {
	if err := validateSchema(data); err != nil {
		return nil, err
	}

	update := &Update{}
	if err := json.Unmarshal(data, update); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", source, err)
	}

	verified, err := validateData(cfg, update, lastUpdate)
	if err != nil {
		return nil, err
	}
	return verified, nil
}

func validateSchema(data []byte) error {
	sch, err := jsonschema.Compile(schemaFile)
	if err != nil {
		return fmt.Errorf("compile bootstrap json schema: %w", err)
	}
	var v any
	if err = json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("unmarshal bootstrap data: %w", err)
	}
	if err = sch.Validate(v); err != nil {
		return fmt.Errorf("validate bootstrap data: %w", err)
	}
	return nil
}

func validateData(cfg Config, update *Update, lastUpdateID uint32) (*VerifiedUpdate, error) {
	if update.Version != cfg.Version {
		return nil, fmt.Errorf("%w: expected %v, got %v", ErrWrongVersion, cfg.Version, update.Version)
	}
	if update.Data.ID <= lastUpdateID {
		return nil, nil
	}

	verified := &VerifiedUpdate{
		ID: update.Data.ID,
	}
	var last uint32
	for _, epochData := range update.Data.Epochs {
		if last == 0 {
			last = epochData.Epoch
		} else if epochData.Epoch <= last {
			return nil, fmt.Errorf("%w: last %v current %v", ErrEpochOutOfOrder, last, epochData.Epoch)
		}

		beaconByte, err := hex.DecodeString(epochData.Beacon)
		if err != nil || len(beaconByte) < types.BeaconSize {
			return nil, fmt.Errorf("%w: %v", ErrInvalidBeacon, epochData.Beacon)
		}
		beacon := types.BytesToBeacon(beaconByte)

		// json schema guarantees the active set has unique members
		activeSet := make([]types.ATXID, 0, len(epochData.ActiveSet))
		for _, atx := range epochData.ActiveSet {
			activeSet = append(activeSet, types.ATXID(types.HexToHash32(atx)))
		}
		verified.Data = append(verified.Data, &EpochOverride{
			Epoch:     types.EpochID(epochData.Epoch),
			Beacon:    beacon,
			ActiveSet: activeSet,
		})
	}
	return verified, nil
}

func load(fs afero.Fs, cfg Config) (*VerifiedUpdate, error) {
	dir, err := bootstrapDir(fs, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	files, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap dir %v: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() > files[j].Name() })
	persisted := filepath.Join(dir, files[0].Name())
	data, err := afero.ReadFile(fs, persisted)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap file %v: %w", persisted, err)
	}
	verified, err := validate(cfg, persisted, data, 0)
	if err != nil {
		return nil, err
	}
	return verified, nil
}

func persist(logger log.Log, fs afero.Fs, cfg Config, id uint32, data []byte) (string, error) {
	if len(cfg.DataDir) == 0 {
		return "", nil
	}
	dir, err := bootstrapDir(fs, cfg.DataDir)
	if err != nil {
		return "", err
	}
	filename := PersistFilename(dir, id)
	if err = afero.WriteFile(fs, filename, data, 0o400); err != nil {
		return "", fmt.Errorf("persist bootstrap: %w", err)
	}
	if err = prune(fs, dir, cfg.NumToKeep); err != nil {
		logger.With().Warning("failed to prune bootstrap files", log.Err(err))
	}
	return filename, nil
}

func prune(fs afero.Fs, dir string, numToKeep int) error {
	files, err := afero.ReadDir(fs, dir)
	if err != nil {
		return err
	}
	if len(files) < numToKeep {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() > files[j].Name() })
	for _, f := range files[numToKeep:] {
		if err = fs.Remove(filepath.Join(dir, f.Name())); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapDir(fs afero.Fs, dataDir string) (string, error) {
	dir := filepath.Join(dataDir, DirName)
	if err := fs.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create bootstrap data dir: %w", err)
	}
	return dir, nil
}

func PersistFilename(dir string, id uint32) string {
	return filepath.Join(dir, fmt.Sprintf("%05d-%v", id, time.Now().UTC().Format(format)))
}
