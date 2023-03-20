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

	timeout    = 5 * time.Second
	schemaFile = "schema.json"
	format     = "2006-01-02T15-04-05"
)

var (
	ErrEpochOutOfOrder = errors.New("epoch out of order")
	ErrWrongVersion    = errors.New("wrong schema version")
	ErrInvalidBeacon   = errors.New("invalid beacon")
)

type realClient struct{}

func (realClient) Query(ctx context.Context, resource *url.URL) ([]byte, error) {
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

type VerifiedUpdate struct {
	Persisted string
	ID        uint32
	Data      []*EpochOverride
}

type EpochOverride struct {
	Epoch     types.EpochID
	Beacon    types.Beacon
	ActiveSet []types.ATXID
}

func (vd *VerifiedUpdate) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("persisted", vd.Persisted)
	for _, epoch := range vd.Data {
		encoder.AddString("beacon", epoch.Beacon.String())
		encoder.AddArray("activeset", log.ArrayMarshalerFunc(func(aencoder log.ArrayEncoder) error {
			for _, atx := range epoch.ActiveSet {
				aencoder.AppendString(atx.String())
			}
			return nil
		}))
	}
	return nil
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
	cfg         Config
	logger      log.Log
	subscribers []Receiver
	latest      *VerifiedUpdate
	fs          afero.Fs
	client      httpclient
	once        sync.Once
	eg          errgroup.Group
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

func New(subs []Receiver, opts ...Opt) (*Updater, error) {
	u := &Updater{
		cfg:         DefaultConfig(),
		logger:      log.NewNop(),
		fs:          afero.NewOsFs(),
		client:      realClient{},
		subscribers: subs,
	}
	for _, opt := range opts {
		opt(u)
	}
	verified, err := load(u.fs, u.cfg)
	if err != nil {
		return nil, err
	}
	if verified != nil {
		u.latest = verified
		notify(verified, subs)
	}
	return u, nil
}

func (u *Updater) Start(ctx context.Context) {
	u.once.Do(func() {
		u.eg.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(u.cfg.Interval):
					if err := u.DoIt(); err != nil {
						u.logger.With().Error("failed to get bootstrap update", log.Err(err))
					}
				}
			}
		})
	})
}

func (u *Updater) Close() {
	u.eg.Wait()
}

func (u *Updater) DoIt() error {
	var lastUpdate uint32
	if u.latest != nil {
		lastUpdate = u.latest.ID
	}
	verified, err := get(u.fs, u.client, u.cfg, lastUpdate)
	if err != nil {
		return err
	}
	if verified == nil { // no new update
		return nil
	}
	u.latest = verified
	u.logger.With().Info("updated bootstrap file", log.Inline(verified))
	notify(verified, u.subscribers)
	if err = prune(u.fs, filepath.Dir(verified.Persisted), u.cfg.NumToKeep); err != nil {
		u.logger.With().Warning("failed to prune bootstrap files", log.Err(err))
	}
	return nil
}

func notify(newUpdate *VerifiedUpdate, subscribers []Receiver) {
	for _, sub := range subscribers {
		sub.OnBoostrapUpdate(newUpdate)
	}
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

func get(fs afero.Fs, client httpclient, cfg Config, lastUpdate uint32) (*VerifiedUpdate, error) {
	var (
		data     []byte
		err      error
		resource *url.URL
	)
	resource, err = url.Parse(cfg.URI)
	if err != nil {
		return nil, fmt.Errorf("parse bootstrap uri: %w", err)
	}
	if resource.Scheme == "https" || resource.Scheme == "http" {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		data, err = client.Query(ctx, resource)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("scheme not supported %v", resource.Scheme)
	}

	verified, err := validate(cfg, resource.String(), data, lastUpdate)
	if err != nil {
		return nil, err
	}
	if verified == nil {
		return nil, nil
	}
	verified.Persisted, err = persist(fs, cfg.DataDir, verified.ID, data)
	if err != nil {
		return nil, err
	}
	return verified, nil
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

func persist(fs afero.Fs, dataDir string, id uint32, data []byte) (string, error) {
	if len(dataDir) == 0 {
		return "", nil
	}
	dir, err := bootstrapDir(fs, dataDir)
	if err != nil {
		return "", err
	}
	filename := PersistFilename(dir, id)
	if err := afero.WriteFile(fs, filename, data, 0o400); err != nil {
		return "", fmt.Errorf("persist bootstrap: %w", err)
	}
	return filename, nil
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
