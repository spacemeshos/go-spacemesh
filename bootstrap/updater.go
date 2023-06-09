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
	"strconv"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	DefaultURL      = "http://localhost:3000/bootstrap"
	DirName         = "bootstrap"
	suffixLen       = 2
	SuffixBeacon    = "bc"
	SuffixActiveSet = "as"
	SuffixBoostrap  = "bs"

	httpTimeout   = 5 * time.Second
	notifyTimeout = time.Second
	schemaFile    = "schema.json"
	format        = "2006-01-02T15-04-05"
)

var (
	ErrWrongVersion  = errors.New("wrong schema version")
	ErrInvalidBeacon = errors.New("invalid beacon")
)

type Config struct {
	URL     string `mapstructure:"bootstrap-url"`
	Version string `mapstructure:"bootstrap-version"`

	DataDir  string
	Interval time.Duration
}

func DefaultConfig() Config {
	return Config{
		URL:      DefaultURL,
		Version:  "https://spacemesh.io/bootstrap.schema.json.1.0",
		DataDir:  os.TempDir(),
		Interval: 30 * time.Second,
	}
}

type Updater struct {
	cfg    Config
	logger log.Log
	clock  layerClock
	fs     afero.Fs
	client *http.Client
	once   sync.Once
	eg     errgroup.Group

	mu          sync.Mutex
	subscribers []chan *VerifiedUpdate
	updates     map[types.EpochID]map[string]struct{}
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

func WithHttpClient(c *http.Client) Opt {
	return func(u *Updater) {
		u.client = c
	}
}

func New(clock layerClock, opts ...Opt) *Updater {
	u := &Updater{
		cfg:     DefaultConfig(),
		logger:  log.NewNop(),
		clock:   clock,
		fs:      afero.NewOsFs(),
		client:  &http.Client{},
		updates: map[types.EpochID]map[string]struct{}{},
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
	loaded, err := load(u.fs, u.cfg, u.clock.CurrentLayer().GetEpoch())
	if err != nil {
		return err
	}
	for _, verified := range loaded {
		if err = u.updateAndNotify(ctx, verified); err != nil {
			return err
		}
		u.logger.With().Info("loaded boostrap file", log.Inline(verified))
		u.addUpdate(verified.Data.Epoch, verified.Persisted[len(verified.Persisted)-suffixLen:])
	}
	return nil
}

func (u *Updater) Start(ctx context.Context) error {
	if len(u.cfg.DataDir) == 0 {
		return fmt.Errorf("data dir not set %s", u.cfg.DataDir)
	}
	u.once.Do(func() {
		u.eg.Go(func() error {
			if err := u.Load(ctx); err != nil {
				return err
			}
			wait := time.Duration(0)
			u.logger.With().Info("start listening to update",
				log.String("source", u.cfg.URL),
				log.Duration("interval", u.cfg.Interval),
			)
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
					ctx := log.WithNewSessionID(ctx)
					if err := u.DoIt(ctx); err != nil {
						updateFailureCount.Add(1)
						u.logger.With().Debug("failed to get bootstrap update", log.Err(err))
					}
				}
				wait = u.cfg.Interval
			}
		})
	})
	return nil
}

func (u *Updater) Close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, ch := range u.subscribers {
		close(ch)
	}
	_ = u.eg.Wait()
}

func (u *Updater) addUpdate(epoch types.EpochID, suffix string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.updates[epoch]; !ok {
		u.updates[epoch] = map[string]struct{}{}
	}
	u.updates[epoch][suffix] = struct{}{}
}

func (u *Updater) downloaded(epoch types.EpochID, suffix string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.updates[epoch]; ok {
		_, ok2 := u.updates[epoch][suffix]
		return ok2
	}
	return false
}

func (u *Updater) DoIt(ctx context.Context) error {
	current := u.clock.CurrentLayer().GetEpoch()
	defer func() {
		if err := u.prune(current); err != nil {
			u.logger.With().Error("failed to prune",
				log.Context(ctx),
				log.Uint32("current epoch", current.Uint32()),
				log.Err(err),
			)
		}
	}()
	for _, epoch := range requiredEpochs(current) {
		verified, cached, err := u.checkEpochUpdate(ctx, epoch, SuffixBoostrap)
		if err != nil {
			return err
		}
		if verified != nil || cached {
			// if we have the bootstrap update, no need to look for others
			continue
		}
		for _, suffix := range []string{SuffixBeacon, SuffixActiveSet} {
			if _, _, err = u.checkEpochUpdate(ctx, epoch, suffix); err != nil {
				return err
			}
		}
	}
	return nil
}

func UpdateName(epoch types.EpochID, suffix string) string {
	return fmt.Sprintf("epoch-%d-update-%s", epoch, suffix)
}

func makeUri(url string, epoch types.EpochID, suffix string) string {
	return fmt.Sprintf("%s/%s", url, UpdateName(epoch, suffix))
}

func (u *Updater) checkEpochUpdate(ctx context.Context, epoch types.EpochID, suffix string) (*VerifiedUpdate, bool, error) {
	uri := makeUri(u.cfg.URL, epoch, suffix)
	if u.downloaded(epoch, suffix) {
		return nil, true, nil
	}
	verified, data, err := u.get(ctx, uri)
	if err != nil {
		return nil, false, err
	}
	if verified == nil { // update doesn't exist
		return nil, false, nil
	}
	u.addUpdate(epoch, suffix)
	filename := PersistFilename(u.cfg.DataDir, epoch, filepath.Base(uri))
	if err = u.fs.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return nil, false, fmt.Errorf("%w: create bootstrap data dir: %s", err, filename)
	}
	if err = afero.WriteFile(u.fs, filename, data, 0o400); err != nil {
		return nil, false, fmt.Errorf("persist bootstrap %s: %w", filename, err)
	}
	verified.Persisted = filename
	u.logger.WithContext(ctx).With().Info("new bootstrap file", log.Inline(verified))
	if err = u.updateAndNotify(ctx, verified); err != nil {
		return verified, false, err
	}
	updateOkCount.Add(1)
	return verified, false, nil
}

func (u *Updater) updateAndNotify(ctx context.Context, verified *VerifiedUpdate) error {
	u.mu.Lock()
	defer u.mu.Unlock()
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

func (u *Updater) get(ctx context.Context, uri string) (*VerifiedUpdate, []byte, error) {
	resource, err := url.Parse(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("parse bootstrap uri: %w", err)
	}
	if resource.Scheme != "https" && resource.Scheme != "http" {
		return nil, nil, fmt.Errorf("scheme not supported %v", resource.Scheme)
	}

	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	t0 := time.Now()
	data, err := query(ctx, u.client, resource)
	if err != nil {
		queryFailureCount.Add(1)
		return nil, nil, err
	}
	queryDuration.WithLabelValues(labelQuery).Observe(float64(time.Since(t0)))
	queryOkCount.Add(1)
	if len(data) == 0 { // no update data
		return nil, nil, nil
	}
	received.Add(float64(len(data)))
	verified, err := validate(u.cfg, resource.String(), data)
	if err != nil {
		return nil, nil, err
	}
	return verified, data, nil
}

func query(ctx context.Context, client *http.Client, resource *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get bootstrap file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bootstrap read resonse: %w", err)
	}
	return data, nil
}

func validate(cfg Config, source string, data []byte) (*VerifiedUpdate, error) {
	if err := ValidateSchema(data); err != nil {
		return nil, err
	}

	update := &Update{}
	if err := json.Unmarshal(data, update); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", source, err)
	}

	verified, err := validateData(cfg, update)
	if err != nil {
		return nil, err
	}
	return verified, nil
}

func ValidateSchema(data []byte) error {
	sch, err := jsonschema.CompileString(schemaFile, Schema)
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

func validateData(cfg Config, update *Update) (*VerifiedUpdate, error) {
	if update.Version != cfg.Version {
		return nil, fmt.Errorf("%w: expected %v, got %v", ErrWrongVersion, cfg.Version, update.Version)
	}
	verified := &VerifiedUpdate{
		Data: &EpochOverride{
			Epoch: types.EpochID(update.Data.Epoch.ID),
		},
	}
	beaconByte, err := hex.DecodeString(update.Data.Epoch.Beacon)
	if err != nil || len(beaconByte) < types.BeaconSize {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBeacon, update.Data.Epoch.Beacon)
	}
	verified.Data.Beacon = types.BytesToBeacon(beaconByte)

	if len(update.Data.Epoch.ActiveSet) > 0 {
		// json schema guarantees the active set has unique members
		activeSet := make([]types.ATXID, 0, len(update.Data.Epoch.ActiveSet))
		for _, atx := range update.Data.Epoch.ActiveSet {
			activeSet = append(activeSet, types.ATXID(types.HexToHash32(atx)))
		}
		verified.Data.ActiveSet = activeSet
	}
	return verified, nil
}

func load(fs afero.Fs, cfg Config, current types.EpochID) ([]*VerifiedUpdate, error) {
	dir := bootstrapDir(cfg.DataDir)
	_, err := fs.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read bootstrap dir %v: %w", dir, err)
	}
	var loaded []*VerifiedUpdate
	for _, epoch := range requiredEpochs(current) {
		edir := epochDir(cfg.DataDir, epoch)
		files, err := afero.ReadDir(fs, edir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("read epoch dir %v: %w", dir, err)
		}
		for _, f := range files {
			persisted := filepath.Join(edir, f.Name())
			data, err := afero.ReadFile(fs, persisted)
			if err != nil {
				return nil, fmt.Errorf("read bootstrap file %v: %w", persisted, err)
			}
			verified, err := validate(cfg, persisted, data)
			if err != nil {
				return nil, err
			}
			verified.Persisted = persisted
			loaded = append(loaded, verified)
		}
	}
	return loaded, nil
}

func requiredEpochs(current types.EpochID) []types.EpochID {
	return []types.EpochID{current - 1, current, current + 1}
}

func (u *Updater) prune(current types.EpochID) error {
	toKeep := map[string]struct{}{}
	required := requiredEpochs(current)
	for _, epoch := range required {
		toKeep[strconv.Itoa(int(epoch))] = struct{}{}
	}
	dir := bootstrapDir(u.cfg.DataDir)
	files, err := afero.ReadDir(u.fs, dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("list bootstrap dir %s: %w", dir, err)
	}
	for _, f := range files {
		if f.IsDir() {
			if _, ok := toKeep[f.Name()]; ok {
				continue
			}
		}
		if err = u.fs.RemoveAll(filepath.Join(dir, f.Name())); err != nil {
			return err
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for epoch := range u.updates {
		if epoch < required[0] {
			delete(u.updates, epoch)
		}
	}
	return nil
}

func bootstrapDir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

func epochDir(dataDir string, epoch types.EpochID) string {
	return filepath.Join(bootstrapDir(dataDir), strconv.Itoa(int(epoch)))
}

func PersistFilename(dataDir string, epoch types.EpochID, basename string) string {
	return filepath.Join(epochDir(dataDir, epoch), fmt.Sprintf("%s-%v", basename, time.Now().UTC().Format(format)))
}
