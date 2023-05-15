package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
)

const (
	recoveryDir = "recovery"
)

type RecoverConfig struct {
	GoldenAtx         types.ATXID
	DataDir           string
	DbFile            string
	DbConnections     int
	DbLatencyMetering bool
}

func RecoveryDir(dataDir string) string {
	return filepath.Join(dataDir, recoveryDir)
}

func RecoveryFilename(dataDir, base string) string {
	return filepath.Join(RecoveryDir(dataDir), base)
}

func ReadCheckpointAndDie(ctx context.Context, logger log.Log, dataDir, uri string) error {
	fs := afero.NewOsFs()
	file, err := copyToLocalFile(ctx, logger, fs, dataDir, uri)
	if err != nil {
		logger.WithContext(ctx).With().Error("failed to copy checkpoint file", log.Err(err))
		return fmt.Errorf("copy checkpoint file before restart: %w", err)
	}
	logger.With().Fatal("restart to recover from checkpoint", log.String("file", file))
	return nil
}

func copyToLocalFile(ctx context.Context, logger log.Log, fs afero.Fs, dataDir, uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("%w: parse recovery URI %v", err, uri)
	}
	dst := RecoveryFilename(dataDir, filepath.Base(parsed.Path))
	if parsed.Scheme == "file" {
		_, err = fs.Stat(parsed.Path)
		if err != nil {
			return "", fmt.Errorf("stat checkpoint file %v: %w", parsed.Path, err)
		}
		if dst != parsed.Path {
			if bdir, err := backupRecovery(fs, RecoveryDir(dataDir)); err != nil {
				return "", err
			} else if bdir != "" {
				logger.WithContext(ctx).With().Info("old recovery data backed up", log.String("dir", bdir))
			}
			if err = copyfile(fs, parsed.Path, dst); err != nil {
				return "", err
			}
			logger.With().Debug("copied file",
				log.String("from", parsed.Path),
				log.String("to", dst),
			)
		}
		return dst, nil
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("uri scheme %s not supported", uri)
	}
	if bdir, err := backupRecovery(fs, RecoveryDir(dataDir)); err != nil {
		return "", err
	} else if bdir != "" {
		logger.WithContext(ctx).With().Info("old recovery data backed up", log.String("dir", bdir))
	}
	if err = httpToLocalFile(ctx, &http.Client{}, parsed, fs, dst); err != nil {
		return "", err
	}
	logger.WithContext(ctx).With().Info("checkpoint data persisted", log.String("file", dst))
	return dst, nil
}

func Recover(ctx context.Context, logger log.Log, fs afero.Fs, cfg *RecoverConfig, nodeID types.NodeID, uri string) (*sql.Database, error) {
	logger.With().Info("Recover from uri", log.String("uri", uri))
	cpfile, err := copyToLocalFile(ctx, logger, fs, cfg.DataDir, uri)
	if err != nil {
		return nil, err
	}
	return recoverFromLocalFile(ctx, logger, fs, cfg, nodeID, cpfile)
}

type recoverydata struct {
	restore  types.LayerID
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
	fs afero.Fs,
	cfg *RecoverConfig,
	nodeID types.NodeID,
	file string,
) (*sql.Database, error) {
	logger.With().Info("recovering from checkpoint file", log.String("file", file))
	data, err := checkpointData(fs, file)
	if err != nil {
		return nil, err
	}
	logger.With().Info("recovery data contains",
		log.Int("num_accounts", len(data.accounts)),
		log.Int("num_atxs", len(data.atxs)),
	)
	dbf := filepath.Join(cfg.DataDir, cfg.DbFile)
	db, err := sql.Open("file:"+dbf,
		sql.WithConnections(cfg.DbConnections),
		sql.WithLatencyMetering(cfg.DbLatencyMetering),
	)
	if err != nil {
		return nil, fmt.Errorf("open old database: %w", err)
	}
	own, err := preserveOwnData(db, cfg, nodeID, data)
	if err != nil {
		return nil, err
	}
	if err = db.Close(); err != nil {
		return nil, fmt.Errorf("close old db: %w", err)
	}

	// all is ready. backup the old data and create new.
	backupDir, err := backupOldDb(fs, cfg.DataDir, cfg.DbFile)
	if err != nil {
		return nil, err
	}
	logger.With().Info("backed up old database", log.String("backup dir", backupDir))

	db, err = sql.Open("file:"+dbf,
		sql.WithConnections(cfg.DbConnections),
		sql.WithLatencyMetering(cfg.DbLatencyMetering),
	)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %w", err)
	}
	logger.With().Info("populating new database",
		log.Int("num accounts", len(data.accounts)),
		log.Int("num atxs", len(data.atxs)),
		log.Int("own atx and deps", len(own.preserve)),
	)
	if err = db.WithTx(ctx, func(tx *sql.Tx) error {
		for _, acct := range data.accounts {
			if err = accounts.Update(tx, acct); err != nil {
				return fmt.Errorf("restore account snapshot: %w", err)
			}
			logger.WithContext(ctx).With().Info("account stored",
				acct.Address,
				log.Uint64("nonce", acct.NextNonce),
				log.Uint64("balance", acct.Balance),
			)
		}
		for _, catx := range data.atxs {
			if err = atxs.AddCheckpointed(tx, catx); err != nil {
				return fmt.Errorf("add checkpoint atx %s: %w", catx.ID.String(), err)
			}
			logger.WithContext(ctx).With().Info("checkpoint atx saved", catx.ID)
		}
		if len(own.preserve) != 0 {
			for _, atx := range own.preserve {
				if err = atxs.Add(tx, atx); err != nil {
					return fmt.Errorf("preserve atx %s: %w", atx.ID().String(), err)
				}
				logger.WithContext(ctx).With().Info("atx preserved", atx.ID())
			}
			ref := types.PoetProofRef(own.atx.GetPoetProofRef())
			var proofMessage types.PoetProofMessage
			if err = codec.Decode(own.proof, &proofMessage); err != nil {
				return fmt.Errorf("deocde proof: %w", err)
			}
			if err = poets.Add(tx, ref, own.proof, proofMessage.PoetServiceID, proofMessage.RoundID); err != nil {
				return fmt.Errorf("add own atx proof %s: %w", own.atx.ID().String(), err)
			}
			logger.WithContext(ctx).With().Info("own atx proof saved", own.atx.ID())
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if _, err = backupRecovery(fs, RecoveryDir(cfg.DataDir)); err != nil {
		return nil, err
	}
	types.SetEffectiveGenesis(data.restore.Uint32() - 1)
	logger.WithContext(ctx).With().Info("effective genesis reset for recovery", types.GetEffectiveGenesis())
	return db, nil
}

func checkpointData(fs afero.Fs, file string) (*recoverydata, error) {
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
			Layer:     types.GetEffectiveGenesis(),
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
		restore:  types.LayerID(checkpoint.Data.Restore),
		accounts: allAccts,
		atxs:     allAtxs,
	}, nil
}
