package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/spacemeshos/post/initialization"
	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/recovery"
)

const (
	recoveryDir = "recovery"
	fileRegex   = "snapshot-(?P<Snapshot>[1-9][0-9]*)-restore-(?P<Restore>[1-9][0-9]*)"
)

type Config struct {
	Uri     string `mapstructure:"recovery-uri"`
	Restore uint32 `mapstructure:"recovery-layer"`

	// set to false if atxs are not compatible before and after the checkpoint recovery.
	PreserveOwnAtx bool `mapstructure:"preserve-own-atx"`

	// only set for systests. recovery from file in $DataDir/recovery
	RecoverFromDefaultDir bool
}

func DefaultConfig() Config {
	return Config{
		PreserveOwnAtx: true,
	}
}

type RecoverConfig struct {
	GoldenAtx      types.ATXID
	PostDataDir    string
	DataDir        string
	DbFile         string
	PreserveOwnAtx bool
	NodeID         types.NodeID
	Uri            string
	Restore        types.LayerID
}

func RecoveryDir(dataDir string) string {
	return filepath.Join(dataDir, recoveryDir)
}

func RecoveryFilename(dataDir, base string, restore types.LayerID) string {
	matches := regexp.MustCompile(fileRegex).FindStringSubmatch(base)
	if len(matches) > 0 {
		return filepath.Join(RecoveryDir(dataDir), base)
	}
	return filepath.Join(RecoveryDir(dataDir), fmt.Sprintf("%s-restore-%d", base, restore.Uint32()))
}

// ParseRestoreLayer parses the restore layer from the filename.
// only used in systests when RecoverFromDefaultDir is true.
// DO NOT USE in production as inferring metadata from filename is not robust and error-prone.
func ParseRestoreLayer(fname string) (types.LayerID, error) {
	matches := regexp.MustCompile(fileRegex).FindStringSubmatch(fname)
	if len(matches) != 3 {
		return 0, fmt.Errorf("unrecogized recovery file %s", fname)
	}
	s, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("parse snapshot layer %s: %w", fname, err)
	}
	r, err := strconv.Atoi(matches[2])
	if err != nil {
		return 0, fmt.Errorf("parse restore layer %s: %w", fname, err)
	}
	snapshot := types.LayerID(s)
	restore := types.LayerID(r)
	if restore <= snapshot {
		return 0, fmt.Errorf("inconsistent restore layer %s: %w", fname, err)
	}
	return restore, nil
}

// ReadCheckpointAndDie copies the checkpoint file from uri and panic to restart
// the node and recover from the checkpoint data just copied.
// only used in systests. only has effect when RecoverFromDefaultDir is true.
func ReadCheckpointAndDie(ctx context.Context, logger log.Log, dataDir, uri string, restore types.LayerID) error {
	fs := afero.NewOsFs()
	file, err := copyToLocalFile(ctx, logger, fs, dataDir, uri, restore)
	if err != nil {
		return fmt.Errorf("copy checkpoint file before restart: %w", err)
	}
	logger.With().Panic("restart to recover from checkpoint", log.String("file", file))
	return nil
}

func copyToLocalFile(ctx context.Context, logger log.Log, fs afero.Fs, dataDir, uri string, restore types.LayerID) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("%w: parse recovery URI %v", err, uri)
	}
	dst := RecoveryFilename(dataDir, filepath.Base(parsed.String()), restore)
	if parsed.Scheme == "file" {
		src := filepath.Join(parsed.Host, parsed.Path)
		_, err = fs.Stat(src)
		if err != nil {
			return "", fmt.Errorf("stat checkpoint file %v: %w", src, err)
		}
		if src != dst {
			if bdir, err := backupRecovery(fs, RecoveryDir(dataDir)); err != nil {
				return "", err
			} else if bdir != "" {
				logger.With().Info("old recovery data backed up",
					log.Context(ctx),
					log.String("dir", bdir),
				)
			}
			if err = CopyFile(fs, src, dst); err != nil {
				return "", err
			}
			logger.With().Debug("copied file",
				log.Context(ctx),
				log.String("from", src),
				log.String("to", dst),
			)
		}
		return dst, nil
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("uri scheme not supported: %s", uri)
	}
	if bdir, err := backupRecovery(fs, RecoveryDir(dataDir)); err != nil {
		return "", err
	} else if bdir != "" {
		logger.With().Info("old recovery data backed up",
			log.Context(ctx),
			log.String("dir", bdir),
		)
	}
	if err = httpToLocalFile(ctx, parsed, fs, dst); err != nil {
		return "", err
	}
	logger.With().Info("checkpoint data persisted",
		log.Context(ctx),
		log.String("file", dst),
	)
	return dst, nil
}

type PreservedData struct {
	Deps   []*types.VerifiedActivationTx
	Proofs []*types.PoetProofMessage
}

func Recover(
	ctx context.Context,
	logger log.Log,
	fs afero.Fs,
	cfg *RecoverConfig,
) (*PreservedData, error) {
	db, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	if err != nil {
		return nil, fmt.Errorf("open old database: %w", err)
	}
	defer db.Close()
	preserve, newdb, err := RecoverWithDb(ctx, logger, db, fs, cfg)
	if err != nil {
		return nil, err
	}
	if err = newdb.Close(); err != nil {
		return nil, fmt.Errorf("close new db: %w", err)
	}
	return preserve, nil
}

func RecoverWithDb(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	fs afero.Fs,
	cfg *RecoverConfig,
) (*PreservedData, *sql.Database, error) {
	oldRestore, err := recovery.CheckpointInfo(db)
	if err != nil {
		return nil, nil, fmt.Errorf("get last checkpoint: %w", err)
	}
	if oldRestore >= cfg.Restore {
		types.SetEffectiveGenesis(oldRestore.Uint32() - 1)
		return nil, nil, nil
	}
	if err = fs.RemoveAll(filepath.Join(cfg.DataDir, bootstrap.DirName)); err != nil {
		return nil, nil, fmt.Errorf("remove old bootstrap data: %w", err)
	}
	logger.With().Info("recover from uri", log.String("uri", cfg.Uri))
	cpfile, err := copyToLocalFile(ctx, logger, fs, cfg.DataDir, cfg.Uri, cfg.Restore)
	if err != nil {
		return nil, nil, err
	}
	return recoverFromLocalFile(ctx, logger, db, fs, cfg, cpfile)
}

type recoverydata struct {
	accounts []*types.Account
	atxs     []*atxs.CheckpointAtx
}

func recoverFromLocalFile(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	fs afero.Fs,
	cfg *RecoverConfig,
	file string,
) (*PreservedData, *sql.Database, error) {
	logger.With().Info("recovering from checkpoint file", log.String("file", file))
	newGenesis := cfg.Restore - 1
	data, err := checkpointData(fs, file, newGenesis)
	if err != nil {
		return nil, nil, err
	}
	logger.With().Info("recovery data contains",
		log.Int("num accounts", len(data.accounts)),
		log.Int("num atxs", len(data.atxs)),
	)
	deps, proofs, err := collectOwnAtxDeps(logger, db, cfg, data)
	if err != nil {
		logger.With().Error("failed to collect deps for own atx", log.Err(err))
		// continue to recover from checkpoint despite failure to preserve own atx
	} else if len(deps) > 0 {
		logger.With().Info("collected own atx deps",
			log.Context(ctx),
			log.Int("own atx deps", len(deps)),
		)
	}
	if err := db.Close(); err != nil {
		return nil, nil, fmt.Errorf("close old db: %w", err)
	}

	// all is ready. backup the old data and create new.
	backupDir, err := backupOldDb(fs, cfg.DataDir, cfg.DbFile)
	if err != nil {
		return nil, nil, err
	}
	logger.With().Info("backed up old database",
		log.Context(ctx),
		log.String("backup dir", backupDir),
	)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite db %w", err)
	}
	logger.With().Info("populating new database",
		log.Context(ctx),
		log.Int("num accounts", len(data.accounts)),
		log.Int("num atxs", len(data.atxs)),
	)
	if err = newdb.WithTx(ctx, func(tx *sql.Tx) error {
		for _, acct := range data.accounts {
			if err = accounts.Update(tx, acct); err != nil {
				return fmt.Errorf("restore account snapshot: %w", err)
			}
			logger.With().Info("account stored",
				log.Context(ctx),
				acct.Address,
				log.Uint64("nonce", acct.NextNonce),
				log.Uint64("balance", acct.Balance),
			)
		}
		for _, catx := range data.atxs {
			if err = atxs.AddCheckpointed(tx, catx); err != nil {
				return fmt.Errorf("add checkpoint atx %s: %w", catx.ID.String(), err)
			}
			logger.With().Info("checkpoint atx saved",
				log.Context(ctx),
				catx.ID,
				catx.SmesherID,
			)
		}
		if err = recovery.SetCheckpoint(tx, cfg.Restore); err != nil {
			return fmt.Errorf("save checkppoint info: %w", err)
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	if _, err = backupRecovery(fs, RecoveryDir(cfg.DataDir)); err != nil {
		return nil, nil, err
	}
	types.SetEffectiveGenesis(newGenesis.Uint32())
	logger.With().Info("effective genesis reset for recovery",
		log.Context(ctx),
		types.GetEffectiveGenesis(),
	)
	var preserve *PreservedData
	if len(deps) > 0 {
		preserve = &PreservedData{Deps: deps, Proofs: proofs}
	}
	return preserve, newdb, nil
}

func checkpointData(fs afero.Fs, file string, newGenesis types.LayerID) (*recoverydata, error) {
	data, err := afero.ReadFile(fs, file)
	if err != nil {
		return nil, fmt.Errorf("%w: read recovery file %v", err, file)
	}
	if err = ValidateSchema(data); err != nil {
		return nil, err
	}
	var checkpoint Checkpoint
	if err = json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("%w: unmarshal checkpoint from %v", err, file)
	}
	if checkpoint.Version != SchemaVersion {
		return nil, fmt.Errorf("expected version %v, got %v", SchemaVersion, checkpoint.Version)
	}

	allAccts := make([]*types.Account, 0, len(checkpoint.Data.Accounts))
	for _, acct := range checkpoint.Data.Accounts {
		a := types.Account{
			Layer:     newGenesis,
			NextNonce: acct.Nonce,
			Balance:   acct.Balance,
			State:     acct.State,
		}
		copy(a.Address[:], acct.Address)
		if acct.Template != nil {
			var tmplAddr types.Address
			copy(tmplAddr[:], acct.Template[:])
			a.TemplateAddress = &tmplAddr
		}
		allAccts = append(allAccts, &a)
	}
	allAtxs := make([]*atxs.CheckpointAtx, 0, len(checkpoint.Data.Atxs))
	for _, atx := range checkpoint.Data.Atxs {
		var catx atxs.CheckpointAtx
		catx.ID = types.ATXID(types.BytesToHash(atx.ID))
		catx.Epoch = types.EpochID(atx.Epoch)
		catx.CommitmentATX = types.ATXID(types.BytesToHash(atx.CommitmentAtx))
		catx.SmesherID = types.BytesToNodeID(atx.PublicKey)
		catx.NumUnits = atx.NumUnits
		catx.VRFNonce = types.VRFPostIndex(atx.VrfNonce)
		catx.BaseTickHeight = atx.BaseTickHeight
		catx.TickCount = atx.TickCount
		catx.Sequence = atx.Sequence
		copy(catx.Coinbase[:], atx.Coinbase)
		allAtxs = append(allAtxs, &catx)
	}
	return &recoverydata{
		accounts: allAccts,
		atxs:     allAtxs,
	}, nil
}

func collectOwnAtxDeps(
	logger log.Log,
	db *sql.Database,
	cfg *RecoverConfig,
	data *recoverydata,
) ([]*types.VerifiedActivationTx, []*types.PoetProofMessage, error) {
	if !cfg.PreserveOwnAtx {
		return nil, nil, nil
	}
	atxid, err := atxs.GetLastIDByNodeID(db, cfg.NodeID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, nil, fmt.Errorf("query own last atx id: %w", err)
	}
	var ref types.ATXID
	var own bool
	if atxid == types.EmptyATXID {
		// check if any atx is being built
		if cfg.PostDataDir == "" {
			return nil, nil, nil
		}
		m, err := initialization.LoadMetadata(cfg.PostDataDir)
		if err != nil {
			return nil, nil, fmt.Errorf("read post metdata %s: %w", cfg.PostDataDir, err)
		}
		ref = types.ATXID(types.BytesToHash(m.CommitmentAtxId))
		logger.With().Debug("found commitment atx from metadata",
			log.Stringer("commitment atx", ref),
		)
	} else {
		ref = atxid
		logger.With().Debug("found own atx",
			log.Stringer("own atx", ref),
		)
		own = true
	}

	all := map[types.ATXID]struct{}{cfg.GoldenAtx: {}, types.EmptyATXID: {}}
	for _, catx := range data.atxs {
		all[catx.ID] = struct{}{}
	}
	logger.With().Info("trying to preserve atx and deps",
		ref,
		log.Bool("own", own),
	)
	return collectDeps(db, cfg.GoldenAtx, ref, all)
}

func collectDeps(
	db *sql.Database,
	goldenAtxId types.ATXID,
	ref types.ATXID,
	all map[types.ATXID]struct{},
) ([]*types.VerifiedActivationTx, []*types.PoetProofMessage, error) {
	var deps []*types.VerifiedActivationTx
	if err := collect(db, goldenAtxId, ref, all, &deps); err != nil {
		return nil, nil, err
	}
	proofs, err := poetProofs(db, deps)
	if err != nil {
		return nil, nil, err
	}
	return deps, proofs, nil
}

func collect(
	db *sql.Database,
	goldenAtxID types.ATXID,
	ref types.ATXID,
	all map[types.ATXID]struct{},
	deps *[]*types.VerifiedActivationTx,
) error {
	if _, ok := all[ref]; ok {
		return nil
	}
	atx, err := atxs.Get(db, ref)
	if err != nil {
		return fmt.Errorf("get ref atx: %w", err)
	}
	if atx.Golden() {
		return fmt.Errorf("atx %v belong to previous snapshot. cannot be preserved", ref)
	}
	if atx.CommitmentATX != nil && *atx.CommitmentATX != goldenAtxID {
		if err = collect(db, goldenAtxID, *atx.CommitmentATX, all, deps); err != nil {
			return err
		}
	} else {
		commitment, err := atxs.CommitmentATX(db, atx.SmesherID)
		if err != nil {
			return fmt.Errorf("get commitment for ref atx %v: %w", ref, err)
		}
		if commitment != goldenAtxID {
			if err = collect(db, goldenAtxID, commitment, all, deps); err != nil {
				return err
			}
		}
	}
	if atx.PrevATXID != types.EmptyATXID {
		if err = collect(db, goldenAtxID, atx.PrevATXID, all, deps); err != nil {
			return err
		}
	}
	if atx.PositioningATX != goldenAtxID {
		if err = collect(db, goldenAtxID, atx.PositioningATX, all, deps); err != nil {
			return err
		}
	}
	*deps = append(*deps, atx)
	all[ref] = struct{}{}
	return nil
}

func poetProofs(db *sql.Database, vatxs []*types.VerifiedActivationTx) ([]*types.PoetProofMessage, error) {
	var proofs []*types.PoetProofMessage
	for _, vatx := range vatxs {
		proof, err := poets.Get(db, types.PoetProofRef(vatx.GetPoetProofRef()))
		if err != nil {
			return nil, fmt.Errorf("get poet proof (%v): %w", vatx.ID(), err)
		}
		var msg types.PoetProofMessage
		if err := codec.Decode(proof, &msg); err != nil {
			return nil, fmt.Errorf("decode poet proof (%v): %w", vatx.ID(), err)
		}
		proofs = append(proofs, &msg)
	}
	return proofs, nil
}
