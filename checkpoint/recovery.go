package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"

	"github.com/spf13/afero"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/atxsync"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/malsync"
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
	DataDir        string
	DbFile         string
	LocalDbFile    string
	PreserveOwnAtx bool
	NodeIDs        []types.NodeID
	Uri            string
	Restore        types.LayerID
}

func RecoveryDir(dataDir string) string {
	return filepath.Join(dataDir, recoveryDir)
}

func RecoveryFilename(dataDir, base string, restore types.LayerID) string {
	return filepath.Join(RecoveryDir(dataDir), fmt.Sprintf("%s-restore-%d", base, restore.Uint32()))
}

func copyToLocalFile(
	ctx context.Context,
	logger log.Log,
	fs afero.Fs,
	dataDir, uri string,
	restore types.LayerID,
) (string, error) {
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

type AtxDep struct {
	ID           types.ATXID
	PublishEpoch types.EpochID
	Blob         []byte
}

type PreservedData struct {
	Deps   []*AtxDep
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
	localDB, err := localsql.Open("file:" + filepath.Join(cfg.DataDir, cfg.LocalDbFile))
	if err != nil {
		return nil, fmt.Errorf("open old local database: %w", err)
	}
	defer localDB.Close()
	logger.With().Info("clearing atx and malfeasance sync metadata from local database")
	if err := localDB.WithTx(ctx, func(tx *sql.Tx) error {
		if err := atxsync.Clear(tx); err != nil {
			return err
		}
		return malsync.Clear(tx)
	}); err != nil {
		return nil, fmt.Errorf("clear atxsync: %w", err)
	}
	preserve, err := RecoverWithDb(ctx, logger, db, localDB, fs, cfg)
	switch {
	case errors.Is(err, ErrCheckpointNotFound):
		logger.With().Info("no checkpoint file available. not recovering",
			log.String("uri", cfg.Uri),
		)
		return nil, nil
	case err != nil:
		return nil, err
	}
	return preserve, nil
}

func RecoverWithDb(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	localDB *localsql.Database,
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
	cpFile, err := copyToLocalFile(ctx, logger, fs, cfg.DataDir, cfg.Uri, cfg.Restore)
	if err != nil {
		return nil, err
	}
	return recoverFromLocalFile(ctx, logger, db, localDB, fs, cfg, cpFile)
}

type recoveryData struct {
	accounts []*types.Account
	atxs     []*atxs.CheckpointAtx
}

func recoverFromLocalFile(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	localDB *localsql.Database,
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
	deps := make(map[types.ATXID]*AtxDep)
	proofs := make(map[types.PoetProofRef]*types.PoetProofMessage)
	if cfg.PreserveOwnAtx {
		logger.With().Info("preserving own atx deps",
			log.Context(ctx),
			log.Int("num identities", len(cfg.NodeIDs)),
		)
		for _, nodeID := range cfg.NodeIDs {
			nodeDeps, nodeProofs, err := collectOwnAtxDeps(logger, db, localDB, nodeID, cfg.GoldenAtx, data)
			if err != nil {
				logger.With().Error("failed to collect deps for own atx",
					nodeID,
					log.Err(err),
				)
				// continue to recover from checkpoint despite failure to preserve own atx
				continue
			}
			logger.With().Info("collected own atx deps",
				log.Context(ctx),
				nodeID,
				log.Int("own atx deps", len(nodeDeps)),
			)
			maps.Copy(deps, nodeDeps)
			maps.Copy(proofs, nodeProofs)
		}
	}

	allDeps := maps.Values(deps)
	// sort ATXs them by publishEpoch and then by ID
	slices.SortFunc(allDeps, func(i, j *AtxDep) int {
		return bytes.Compare(i.ID.Bytes(), j.ID.Bytes())
	})
	slices.SortStableFunc(allDeps, func(i, j *AtxDep) int {
		return int(i.PublishEpoch) - int(j.PublishEpoch)
	})
	allProofs := make([]*types.PoetProofMessage, 0, len(proofs))
	for _, dep := range allDeps {
		poetProofRef, err := poetProofRef(context.Background(), db, dep.ID)
		if err != nil {
			return nil, fmt.Errorf("get poet proof ref (%v): %w", dep.ID, err)
		}
		proof, ok := proofs[poetProofRef]
		if !ok {
			return nil, fmt.Errorf("missing poet proof for atx %v", dep.ID)
		}
		allProofs = append(allProofs, proof)
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

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %w", err)
	}
	defer newDB.Close()
	logger.With().Info("populating new database",
		log.Context(ctx),
		log.Int("num accounts", len(data.accounts)),
		log.Int("num atxs", len(data.atxs)),
	)
	if err = newDB.WithTx(ctx, func(tx *sql.Tx) error {
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
		for _, cAtx := range data.atxs {
			if err = atxs.AddCheckpointed(tx, cAtx); err != nil {
				return fmt.Errorf("add checkpoint atx %s: %w", cAtx.ID.String(), err)
			}
			logger.With().Info("checkpoint atx saved",
				log.Context(ctx),
				cAtx.ID,
				cAtx.SmesherID,
			)
		}
		if err = recovery.SetCheckpoint(tx, cfg.Restore); err != nil {
			return fmt.Errorf("save checkpoint info: %w", err)
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
	if len(allDeps) > 0 {
		preserve = &PreservedData{Deps: allDeps, Proofs: allProofs}
	}
	return preserve, nil
}

func checkpointData(fs afero.Fs, file string, newGenesis types.LayerID) (*recoveryData, error) {
	data, err := afero.ReadFile(fs, file)
	if err != nil {
		return nil, fmt.Errorf("%w: read recovery file %v", err, file)
	}
	if err := ValidateSchema(data); err != nil {
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
		var cAtx atxs.CheckpointAtx
		cAtx.ID = types.ATXID(types.BytesToHash(atx.ID))
		cAtx.Epoch = types.EpochID(atx.Epoch)
		cAtx.CommitmentATX = types.ATXID(types.BytesToHash(atx.CommitmentAtx))
		cAtx.SmesherID = types.BytesToNodeID(atx.PublicKey)
		cAtx.NumUnits = atx.NumUnits
		cAtx.VRFNonce = types.VRFPostIndex(atx.VrfNonce)
		cAtx.BaseTickHeight = atx.BaseTickHeight
		cAtx.TickCount = atx.TickCount
		cAtx.Sequence = atx.Sequence
		copy(cAtx.Coinbase[:], atx.Coinbase)
		allAtxs = append(allAtxs, &cAtx)
	}
	return &recoveryData{
		accounts: allAccts,
		atxs:     allAtxs,
	}, nil
}

func collectOwnAtxDeps(
	logger log.Log,
	db *sql.Database,
	localDB *localsql.Database,
	nodeID types.NodeID,
	goldenATX types.ATXID,
	data *recoveryData,
) (map[types.ATXID]*AtxDep, map[types.PoetProofRef]*types.PoetProofMessage, error) {
	atxid, err := atxs.GetLastIDByNodeID(db, nodeID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, nil, fmt.Errorf("query own last atx id: %w", err)
	}
	var ref types.ATXID
	var own bool
	if atxid != types.EmptyATXID {
		ref = atxid
		logger.With().Debug("found own atx", log.Stringer("own atx", ref))
		own = true
	}

	// check for if smesher is building any atx
	nipostCh, _ := nipost.Challenge(localDB, nodeID)
	if ref == types.EmptyATXID {
		if nipostCh == nil {
			return nil, nil, nil
		}
		if nipostCh.CommitmentATX != nil {
			ref = *nipostCh.CommitmentATX
		}
	}

	all := map[types.ATXID]struct{}{goldenATX: {}, types.EmptyATXID: {}}
	for _, cAtx := range data.atxs {
		all[cAtx.ID] = struct{}{}
	}
	var (
		deps   map[types.ATXID]*AtxDep
		proofs map[types.PoetProofRef]*types.PoetProofMessage
	)
	if ref != types.EmptyATXID {
		logger.With().Info("collecting atx and deps",
			ref,
			log.Bool("own", own),
		)
		deps, proofs, err = collectDeps(db, ref, all)
		if err != nil {
			return nil, nil, err
		}
		logger.With().Debug("collected atx and deps",
			ref,
			log.Int("deps", len(deps)),
		)
	}
	if nipostCh != nil {
		logger.With().Info("collecting pending atx and deps", log.Object("nipost", nipostCh))
		// any previous atx in nipost should already be captured earlier
		// we only care about positioning atx here
		deps2, proofs2, err := collectDeps(db, nipostCh.PositioningATX, all)
		if err != nil {
			return nil, nil, fmt.Errorf("deps from nipost positioning atx (%v): %w", nipostCh.PositioningATX, err)
		}
		maps.Copy(deps, deps2)
		maps.Copy(proofs, proofs2)
	}
	return deps, proofs, nil
}

func collectDeps(
	db *sql.Database,
	ref types.ATXID,
	all map[types.ATXID]struct{},
) (map[types.ATXID]*AtxDep, map[types.PoetProofRef]*types.PoetProofMessage, error) {
	deps := make(map[types.ATXID]*AtxDep)
	if err := collect(db, ref, all, deps); err != nil {
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
	ref types.ATXID,
	all map[types.ATXID]struct{},
	deps map[types.ATXID]*AtxDep,
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
		if err = collect(db, *atx.CommitmentATX, all, deps); err != nil {
			return err
		}
	} else {
		commitment, err := atxs.CommitmentATX(db, atx.SmesherID)
		if err != nil {
			return fmt.Errorf("get commitment for ref atx %v: %w", ref, err)
		}
		if err = collect(db, commitment, all, deps); err != nil {
			return err
		}
	}
	if err = collect(db, atx.PrevATXID, all, deps); err != nil {
		return err
	}

	posAtx, err := positioningATX(context.Background(), db, ref)
	if err != nil {
		return fmt.Errorf("get positioning atx for atx %v: %w", ref, err)
	}
	if err = collect(db, posAtx, all, deps); err != nil {
		return err
	}
	var blob sql.Blob
	err = atxs.LoadBlob(context.Background(), db, ref.Bytes(), &blob)
	if err != nil {
		return fmt.Errorf("load atx blob %v: %w", ref, err)
	}

	deps[ref] = &AtxDep{
		ID:           ref,
		PublishEpoch: atx.PublishEpoch,
		Blob:         blob.Bytes,
	}
	all[ref] = struct{}{}
	return nil
}

func poetProofs(
	db *sql.Database,
	atxIds map[types.ATXID]*AtxDep,
) (map[types.PoetProofRef]*types.PoetProofMessage, error) {
	proofs := make(map[types.PoetProofRef]*types.PoetProofMessage, len(atxIds))
	for atx := range atxIds {
		ref, err := poetProofRef(context.Background(), db, atx)
		if err != nil {
			return nil, fmt.Errorf("get poet proof ref: %w", err)
		}
		proof, err := poets.Get(db, ref)
		if err != nil {
			return nil, fmt.Errorf("get poet proof (atx: %v): %w", atx, err)
		}
		var msg types.PoetProofMessage
		if err := codec.Decode(proof, &msg); err != nil {
			return nil, fmt.Errorf("decode poet proof (%v): %w", atx, err)
		}
		proofs[ref] = &msg
	}
	return proofs, nil
}
