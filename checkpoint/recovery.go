package checkpoint

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"

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
	PreserveOwnAtx bool

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
	dst := RecoveryFilename(dataDir, filepath.Base(parsed.Path), restore)
	if parsed.Scheme == "file" {
		_, err = fs.Stat(parsed.Path)
		if err != nil {
			return "", fmt.Errorf("stat checkpoint file %v: %w", parsed.Path, err)
		}
		if parsed.Path != dst {
			if bdir, err := backupRecovery(fs, RecoveryDir(dataDir)); err != nil {
				return "", err
			} else if bdir != "" {
				logger.With().Info("old recovery data backed up",
					log.Context(ctx),
					log.String("dir", bdir),
				)
			}
			if err = CopyFile(fs, parsed.Path, dst); err != nil {
				return "", err
			}
			logger.With().Debug("copied file",
				log.Context(ctx),
				log.String("from", parsed.Path),
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
	newdb, err := RecoverWithDb(ctx, logger, db, fs, cfg, nodeID, uri, restore)
	if err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return fmt.Errorf("close old db: %w", err)
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

type ownAtxData struct {
	atx      *types.VerifiedActivationTx
	preserve []*types.VerifiedActivationTx
	proof    []byte
}

func preserveOwnData(
	db *sql.Database,
	cfg *RecoverConfig,
	nodeID types.NodeID,
	data *recoverydata,
) (ownAtxData, error) {
	var own ownAtxData
	if !cfg.PreserveOwnAtx {
		return own, nil
	}
	atxid, err := atxs.GetLastIDByNodeID(db, nodeID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return own, fmt.Errorf("query own last atx id: %w", err)
	}
	if atxid == types.EmptyATXID {
		return own, nil
	}
	all := map[types.ATXID]struct{}{}
	for _, catx := range data.atxs {
		all[catx.ID] = struct{}{}
	}
	if _, ok := all[atxid]; ok {
		return own, nil
	}

	own.atx, err = atxs.Get(db, atxid)
	if err != nil {
		return own, fmt.Errorf("get own atx: %w", err)
	}
	own.preserve = append(own.preserve, own.atx)
	deps := map[types.ATXID]struct{}{own.atx.PrevATXID: {}}
	deps[own.atx.PositioningATX] = struct{}{}
	if own.atx.CommitmentATX != nil {
		deps[*own.atx.CommitmentATX] = struct{}{}
	}
	for id := range deps {
		if id == types.EmptyATXID || id == cfg.GoldenAtx {
			continue
		}
		if _, ok := all[id]; ok {
			continue
		}
		dep, err := atxs.Get(db, id)
		if err != nil {
			return own, fmt.Errorf("get dep atx: %w", err)
		}
		own.preserve = append(own.preserve, dep)
	}
	own.proof, err = poets.Get(db, types.PoetProofRef(own.atx.GetPoetProofRef()))
	if err != nil {
		return own, fmt.Errorf("get own atx proof: %w", err)
	}
	return own, nil
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
	own, err := preserveOwnData(db, cfg, nodeID, data)
	if err != nil {
		return nil, err
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
		log.Int("own atx and deps", len(own.preserve)),
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
		if len(own.preserve) != 0 {
			for _, atx := range own.preserve {
				if err = atxs.Add(tx, atx); err != nil {
					return fmt.Errorf("preserve atx %s: %w", atx.ID().String(), err)
				}
				logger.With().Info("atx preserved",
					log.Context(ctx),
					atx.ID(),
				)
			}
			ref := types.PoetProofRef(own.atx.GetPoetProofRef())
			var proofMessage types.PoetProofMessage
			if err = codec.Decode(own.proof, &proofMessage); err != nil {
				return fmt.Errorf("deocde proof: %w", err)
			}
			if err = poets.Add(tx, ref, own.proof, proofMessage.PoetServiceID, proofMessage.RoundID); err != nil {
				return fmt.Errorf("add own atx proof %s: %w", own.atx.ID().String(), err)
			}
			logger.With().Info("own atx proof saved",
				log.Context(ctx),
				log.String("poet service id", hex.EncodeToString(proofMessage.PoetServiceID)),
				log.String("poet round id", proofMessage.RoundID),
				own.atx.ID(),
			)
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
