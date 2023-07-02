package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/spacemeshos/post/initialization"
	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/activation"
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

const recoveryDir = "recovery"

type Config struct {
	Uri     string `mapstructure:"recovery-uri"`
	Restore uint32 `mapstructure:"recovery-layer"`

	// set to false if atxs are not compatible before and after the checkpoint recovery.
	PreserveOwnAtx bool `mapstructure:"preserve-own-atx"`
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
	return filepath.Join(RecoveryDir(dataDir), fmt.Sprintf("%s-restore-%d", base, restore.Uint32()))
}

func copyToLocalFile(ctx context.Context, logger log.Log, fs afero.Fs, dataDir, uri string, restore types.LayerID) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("%w: parse recovery URI %v", err, uri)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: %s", ErrUrlSchemeNotSupported, uri)
	}
	if bdir, err := backupRecovery(fs, RecoveryDir(dataDir)); err != nil {
		return "", err
	} else if bdir != "" {
		logger.With().Info("old recovery data backed up",
			log.Context(ctx),
			log.String("dir", bdir),
		)
	}
	dst := RecoveryFilename(dataDir, filepath.Base(parsed.String()), restore)
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
	preserve, err := RecoverWithDb(ctx, logger, db, fs, cfg)
	if err != nil {
		if errors.Is(err, ErrCheckpointNotFound) {
			logger.With().Info("no checkpoint file available. not recovering",
				log.String("uri", cfg.Uri),
			)
			return nil, nil
		}
		return nil, err
	}
	return preserve, nil
}

func RecoverWithDb(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	fs afero.Fs,
	cfg *RecoverConfig,
) (*PreservedData, error) {
	oldRestore, err := recovery.CheckpointInfo(db)
	if err != nil {
		return nil, fmt.Errorf("get last checkpoint: %w", err)
	}
	if oldRestore >= cfg.Restore {
		types.SetEffectiveGenesis(oldRestore.Uint32() - 1)
		return nil, nil
	}
	if err = fs.RemoveAll(filepath.Join(cfg.DataDir, bootstrap.DirName)); err != nil {
		return nil, fmt.Errorf("remove old bootstrap data: %w", err)
	}
	logger.With().Info("recover from uri", log.String("uri", cfg.Uri))
	cpfile, err := copyToLocalFile(ctx, logger, fs, cfg.DataDir, cfg.Uri, cfg.Restore)
	if err != nil {
		return nil, err
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
) (*PreservedData, error) {
	logger.With().Info("recovering from checkpoint file", log.String("file", file))
	newGenesis := cfg.Restore - 1
	data, err := checkpointData(fs, file, newGenesis)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("close old db: %w", err)
	}

	// all is ready. backup the old data and create new.
	backupDir, err := backupOldDb(fs, cfg.DataDir, cfg.DbFile)
	if err != nil {
		return nil, err
	}
	logger.With().Info("backed up old database",
		log.Context(ctx),
		log.String("backup dir", backupDir),
	)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %w", err)
	}
	defer newdb.Close()
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
		return nil, err
	}
	if _, err = backupRecovery(fs, RecoveryDir(cfg.DataDir)); err != nil {
		return nil, err
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
	return preserve, nil
}

func checkpointData(fs afero.Fs, file string, newGenesis types.LayerID) (*recoverydata, error) {
	data, err := afero.ReadFile(fs, file)
	if err != nil {
		return nil, fmt.Errorf("%w: read recovery file %v", err, file)
	}
	if err = ValidateSchema(data); err != nil {
		return nil, err
	}
	var checkpoint types.Checkpoint
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
		if m, _ := initialization.LoadMetadata(cfg.PostDataDir); m != nil {
			ref = types.ATXID(types.BytesToHash(m.CommitmentAtxId))
			logger.With().Debug("found commitment atx from metadata",
				log.Stringer("commitment atx", ref),
			)
		}
	} else {
		ref = atxid
		logger.With().Debug("found own atx",
			log.Stringer("own atx", ref),
		)
		own = true
	}

	// check for if miner is building any atx
	nipostCh, _ := activation.LoadNipostChallenge(cfg.PostDataDir)
	if ref == types.EmptyATXID && nipostCh == nil {
		return nil, nil, nil
	}

	all := map[types.ATXID]struct{}{cfg.GoldenAtx: {}, types.EmptyATXID: {}}
	for _, catx := range data.atxs {
		all[catx.ID] = struct{}{}
	}
	var (
		deps   []*types.VerifiedActivationTx
		proofs []*types.PoetProofMessage
	)
	if ref != types.EmptyATXID {
		logger.With().Info("collecting atx and deps",
			ref,
			log.Bool("own", own),
		)
		deps, proofs, err = collectDeps(db, cfg.GoldenAtx, ref, all)
		if err != nil {
			return nil, nil, err
		}
	}
	if nipostCh != nil {
		logger.With().Info("collecting pending atx and deps",
			log.Object("nipost", nipostCh),
		)
		// any previous atx in nipost should already be captured earlier
		// we only care about positioning atx here
		deps2, proofs2, err := collectDeps(db, cfg.GoldenAtx, nipostCh.PositioningATX, all)
		if err != nil {
			return nil, nil, fmt.Errorf("deps from nipost positioning atx (%v): %w", nipostCh.PositioningATX, err)
		}
		deps = append(deps, deps2...)
		proofs = append(proofs, proofs2...)
	}
	return deps, proofs, nil
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
	if atx.CommitmentATX != nil {
		if err = collect(db, goldenAtxID, *atx.CommitmentATX, all, deps); err != nil {
			return err
		}
	} else {
		commitment, err := atxs.CommitmentATX(db, atx.SmesherID)
		if err != nil {
			return fmt.Errorf("get commitment for ref atx %v: %w", ref, err)
		}
		if err := collect(db, goldenAtxID, commitment, all, deps); err != nil {
			return err
		}
	}
	if err = collect(db, goldenAtxID, atx.PrevATXID, all, deps); err != nil {
		return err
	}
	if err = collect(db, goldenAtxID, atx.PositioningATX, all, deps); err != nil {
		return err
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
