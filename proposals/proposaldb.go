package proposals

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// DB holds all data for proposals.
type DB struct {
	logger log.Log
	msh    meshDB

	mu        sync.Mutex
	proposals database.Database
	layers    database.Database
}

// dbOpt for configuring DB.
type dbOpt func(*DB)

func withProposalDB(d database.Database) dbOpt {
	return func(db *DB) {
		db.proposals = d
	}
}

func withLayerDB(d database.Database) dbOpt {
	return func(db *DB) {
		db.layers = d
	}
}

func withMeshDB(msh meshDB) dbOpt {
	return func(db *DB) {
		db.msh = msh
	}
}

func withLogger(l log.Log) dbOpt {
	return func(db *DB) {
		db.logger = l
	}
}

func newDB(opts ...dbOpt) *DB {
	db := &DB{
		logger: log.NewNop(),
	}
	for _, opt := range opts {
		opt(db)
	}
	return db
}

// NewProposalDB returns a new DB for proposals.
func NewProposalDB(path string, msh meshDB, logger log.Log) (*DB, error) {
	pdb, err := database.NewLDBDatabase(filepath.Join(path, "proposals"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize proposals db: %w", err)
	}
	ldb, err := database.NewLDBDatabase(filepath.Join(path, "proposal_layers"), 0, 0, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize proposal layers db: %w", err)
	}
	return newDB(withProposalDB(pdb), withLayerDB(ldb), withMeshDB(msh), withLogger(logger)), nil
}

// Close closes all resources.
func (db *DB) Close() {
	db.proposals.Close()
	db.layers.Close()
}

// HasProposal returns true if the database has the Proposal specified by the ProposalID and false otherwise.
func (db *DB) HasProposal(id types.ProposalID) bool {
	has, err := db.proposals.Has(id.Bytes())
	return err == nil && has
}

// AddProposal adds a proposal to the database.
func (db *DB) AddProposal(ctx context.Context, p *types.Proposal) error {
	if err := db.msh.AddBallot(&p.Ballot); err != nil {
		return fmt.Errorf("proposal add ballot: %w", err)
	}
	if err := db.msh.AddTXsFromProposal(ctx, p.LayerIndex, p.ID(), p.TxIDs); err != nil {
		return fmt.Errorf("proposal add TXs: %w", err)
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	dbp := &types.DBProposal{
		ID:         p.ID(),
		BallotID:   p.Ballot.ID(),
		LayerIndex: p.LayerIndex,
		TxIDs:      p.TxIDs,
		Signature:  p.Signature,
	}
	if data, err := codec.Encode(dbp); err != nil {
		return fmt.Errorf("could not encode DBProposal: %w", err)
	} else if err := db.proposals.Put(p.ID().Bytes(), data); err != nil {
		return fmt.Errorf("could not add DBProposal %v to database: %w", p.ID(), err)
	} else if err := db.updateLayer(p); err != nil {
		return err
	}
	events.ReportNewProposal(p)
	db.logger.With().Info("added proposal to database", log.Inline(p))
	return nil
}

func (db *DB) updateLayer(p *types.Proposal) error {
	var b bytes.Buffer
	b.Write(p.LayerIndex.Bytes())
	b.Write(p.ID().Bytes())

	if err := db.layers.Put(b.Bytes(), nil); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}
	return nil
}

// GetProposal retrieves a proposal from the database.
func (db *DB) GetProposal(id types.ProposalID) (*types.Proposal, error) {
	data, err := db.proposals.Get(id.Bytes())
	if err != nil {
		return nil, fmt.Errorf("get from DB: %w", err)
	}
	dbp := &types.DBProposal{}
	if err := codec.Decode(data, dbp); err != nil {
		return nil, fmt.Errorf("parse proposal: %w", err)
	}
	ballot, err := db.msh.GetBallot(dbp.BallotID)
	if err != nil {
		return nil, fmt.Errorf("get ballot from mesh: %w", err)
	}
	return dbp.ToProposal(ballot), nil
}

// GetProposals retrieves multiple proposals from the database.
func (db *DB) GetProposals(pids []types.ProposalID) ([]*types.Proposal, error) {
	proposals := make([]*types.Proposal, 0, len(pids))
	var (
		p   *types.Proposal
		err error
	)
	for _, pid := range pids {
		if p, err = db.GetProposal(pid); err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, nil
}

// Get Proposal encoded in byte using ProposalID hash.
func (db *DB) Get(hash []byte) ([]byte, error) {
	id := types.ProposalID(types.BytesToHash(hash).ToHash20())
	p, err := db.GetProposal(id)
	if err != nil {
		return nil, fmt.Errorf("get proposal: %w", err)
	}

	data, err := codec.Encode(p)
	if err != nil {
		return data, fmt.Errorf("serialize proposal: %w", err)
	}

	return data, nil
}

// LayerProposalIDs retrieves all proposal IDs from the layer specified by layer ID.
func (db *DB) LayerProposalIDs(lid types.LayerID) ([]types.ProposalID, error) {
	var ids []types.ProposalID
	if err := mesh.LayerIDs(db.layers, "", lid, func(id []byte) error {
		var pid types.ProposalID
		copy(pid[:], id)
		ids = append(ids, pid)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("layer proposals IDs: %w", err)
	}

	return ids, nil
}

// LayerProposals retrieves all proposals from the layer specified by layer ID.
func (db *DB) LayerProposals(lid types.LayerID) ([]*types.Proposal, error) {
	var proposals []*types.Proposal
	if err := mesh.LayerIDs(db.layers, "", lid, func(id []byte) error {
		var pid types.ProposalID
		copy(pid[:], id)
		block, err := db.GetProposal(pid)
		if err != nil {
			return err
		}
		proposals = append(proposals, block)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("layer proposals: %w", err)
	}
	return proposals, nil
}
