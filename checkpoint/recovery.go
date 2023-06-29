package checkpoint

import (
	"container/list"
	"context"
	"encoding/hex"
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

func Recover(
	ctx context.Context,
	logger log.Log,
	fs afero.Fs,
	cfg *RecoverConfig,
	nodeID types.NodeID,
	uri string,
	restore types.LayerID,
) error {
	db, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	if err != nil {
		return fmt.Errorf("open old database: %w", err)
	}
	defer db.Close()
	newdb, err := RecoverWithDb(ctx, logger, db, fs, cfg, nodeID, uri, restore)
	if err != nil {
		return err
	}
	if newdb != nil {
		if err = newdb.Close(); err != nil {
			return fmt.Errorf("close new db: %w", err)
		}
	}
	return nil
}

func RecoverWithDb(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	fs afero.Fs,
	cfg *RecoverConfig,
	nodeID types.NodeID,
	uri string,
	restore types.LayerID,
) (*sql.Database, error) {
	oldRestore, err := recovery.CheckpointInfo(db)
	if err != nil {
		return nil, fmt.Errorf("get last checkpoint: %w", err)
	}
	if oldRestore >= restore {
		types.SetEffectiveGenesis(oldRestore.Uint32() - 1)
		return nil, nil
	}
	if err = fs.RemoveAll(filepath.Join(cfg.DataDir, bootstrap.DirName)); err != nil {
		return nil, fmt.Errorf("remove old bootstrap data: %w", err)
	}
	logger.With().Info("recover from uri", log.String("uri", uri))
	cpfile, err := copyToLocalFile(ctx, logger, fs, cfg.DataDir, uri, restore)
	if err != nil {
		return nil, err
	}
	return recoverFromLocalFile(ctx, logger, db, fs, cfg, nodeID, cpfile, restore)
}

type recoverydata struct {
	accounts []*types.Account
	atxs     []*atxs.CheckpointAtx
}

func preserveOwnData(
	logger log.Log,
	db *sql.Database,
	cfg *RecoverConfig,
	nodeID types.NodeID,
	data *recoverydata,
) ([]*types.VerifiedActivationTx, [][]byte, error) {
	if !cfg.PreserveOwnAtx {
		return nil, nil, nil
	}
	atxid, err := atxs.GetLastIDByNodeID(db, nodeID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, nil, fmt.Errorf("query own last atx id: %w", err)
	}
	var seed types.ATXID
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
		seed = types.ATXID(types.BytesToHash(m.CommitmentAtxId))
		logger.With().Debug("found commitment atx from metadata", seed)
	} else {
		seed = atxid
		logger.With().Debug("found own atx", seed)
		own = true
	}

	all := map[types.ATXID]struct{}{cfg.GoldenAtx: {}, types.EmptyATXID: {}}
	for _, catx := range data.atxs {
		all[catx.ID] = struct{}{}
	}
	if _, ok := all[seed]; ok {
		return nil, nil, nil
	}
	seedAtx, err := atxs.Get(db, seed)
	if err != nil {
		return nil, nil, fmt.Errorf("get seed atx: %w", err)
	}

	logger.With().Info("trying to preserve atx and deps",
		seed,
		log.Bool("own", own),
	)
	return collectDeps(logger, db, seedAtx, all)
}

func collectDeps(
	logger log.Log,
	db *sql.Database,
	seed *types.VerifiedActivationTx,
	all map[types.ATXID]struct{},
) ([]*types.VerifiedActivationTx, [][]byte, error) {
	var (
		commitment    types.ATXID
		commitmentAtx *types.VerifiedActivationTx
		err           error
	)
	// there is only be one commitment atx for the whole chain
	if seed.CommitmentATX != nil {
		commitment = *seed.CommitmentATX
	} else {
		commitment, err = atxs.CommitmentATX(db, seed.SmesherID)
		if err != nil {
			return nil, nil, fmt.Errorf("get commitment atx for seed %v: %w", seed.ID(), err)
		}
	}
	if _, ok := all[commitment]; !ok {
		commitmentAtx, err = atxs.Get(db, commitment)
		if err != nil {
			if errors.Is(err, sql.ErrNotFound) {
				logger.With().Warning("commitment atx not present for seed. cannot be preserved",
					log.Stringer("seed", seed.ID()),
					log.Stringer("commitment", commitment),
				)
				return nil, nil, nil
			}
			return nil, nil, fmt.Errorf("get commitment atx %v: %w", commitment, err)
		}
	}

	queue := list.New()
	queue.PushBack(seed)
	deps := []*types.VerifiedActivationTx{seed}
	if commitmentAtx != nil {
		queue.PushBack(commitmentAtx)
		deps = append(deps, commitmentAtx)
		all[commitment] = struct{}{}
	}
	for {
		if queue.Len() == 0 {
			break
		}
		vatx := queue.Remove(queue.Front()).(*types.VerifiedActivationTx)
		if vatx.Golden() {
			logger.With().Warning("atx deps traced to previous snapshot. cannot be preserved",
				log.Stringer("seed", seed.ID()),
				log.Stringer("dep", vatx.ID()),
			)
			return nil, nil, nil
		}
		for _, id := range []types.ATXID{vatx.PrevATXID, vatx.PositioningATX} {
			if _, ok := all[id]; !ok {
				dep, err := atxs.Get(db, id)
				if err != nil {
					if errors.Is(err, sql.ErrNotFound) {
						logger.With().Warning("atx dep not present. cannot be preserved",
							log.Stringer("seed", seed.ID()),
							log.Stringer("dep", id),
						)
						return nil, nil, nil
					}
					return nil, nil, fmt.Errorf("get dep atx %v: %w", id, err)
				}
				deps = append(deps, dep)
				all[id] = struct{}{}
				queue.PushBack(dep)
				logger.With().Debug("added atx dep", dep.ID())
			}
		}
	}
	proofs, err := poetProofs(db, deps)
	if err != nil {
		return nil, nil, err
	}
	return deps, proofs, nil
}

func poetProofs(db *sql.Database, vatxs []*types.VerifiedActivationTx) ([][]byte, error) {
	var proofs [][]byte
	for _, vatx := range vatxs {
		proof, err := poets.Get(db, types.PoetProofRef(vatx.GetPoetProofRef()))
		if err != nil {
			return nil, fmt.Errorf("get atx proof (%v): %w", vatx.ID(), err)
		}
		proofs = append(proofs, proof)
	}
	return proofs, nil
}

func recoverFromLocalFile(
	ctx context.Context,
	logger log.Log,
	db *sql.Database,
	fs afero.Fs,
	cfg *RecoverConfig,
	nodeID types.NodeID,
	file string,
	restore types.LayerID,
) (*sql.Database, error) {
	logger.With().Info("recovering from checkpoint file", log.String("file", file))
	newGenesis := restore - 1
	data, err := checkpointData(fs, file, newGenesis)
	if err != nil {
		return nil, err
	}
	logger.With().Info("recovery data contains",
		log.Int("num_accounts", len(data.accounts)),
		log.Int("num_atxs", len(data.atxs)),
	)
	deps, proofs, err := preserveOwnData(logger, db, cfg, nodeID, data)
	if err != nil {
		logger.With().Error("failed to preserve own atx", log.Err(err))
		// continue to recover from checkpoint despite failure to preserve own atx
	} else if len(deps) > 0 {
		logger.With().Info("will preserve own atx",
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
	logger.With().Info("populating new database",
		log.Context(ctx),
		log.Int("num accounts", len(data.accounts)),
		log.Int("num atxs", len(data.atxs)),
		log.Int("own atx deps", len(deps)),
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
		if len(deps) > 0 {
			logger.With().Info("preserving deps for own atx",
				log.Context(ctx),
				log.Int("num atx", len(deps)),
			)
			for i, atx := range deps {
				if err = atxs.Add(tx, atx); err != nil {
					return fmt.Errorf("preserve atx %s: %w", atx.ID().String(), err)
				}
				logger.With().Info("atx preserved",
					log.Context(ctx),
					atx.ID(),
				)
				proof := proofs[i]
				ref := types.PoetProofRef(atx.GetPoetProofRef())
				var proofMessage types.PoetProofMessage
				if err = codec.Decode(proof, &proofMessage); err != nil {
					return fmt.Errorf("deocde proof: %w", err)
				}
				if err = poets.Add(tx, ref, proof, proofMessage.PoetServiceID, proofMessage.RoundID); err != nil {
					return fmt.Errorf("add atx proof %s: %w", atx.ID(), err)
				}
				logger.With().Info("poet proof saved",
					log.Context(ctx),
					atx.ID(),
					log.String("poet service id", hex.EncodeToString(proofMessage.PoetServiceID)),
					log.String("poet round id", proofMessage.RoundID),
				)
			}
		}
		if err = recovery.SetCheckpoint(tx, restore); err != nil {
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
	return newdb, nil
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
