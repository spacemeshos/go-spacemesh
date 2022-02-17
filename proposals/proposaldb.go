package proposals

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
)

// DB holds all data for proposals.
type DB struct {
	logger log.Log
	msh    meshDB
	sqlDB  *sql.Database
}

// dbOpt for configuring DB.
type dbOpt func(*DB)

func withSQLDB(d *sql.Database) dbOpt {
	return func(db *DB) {
		db.sqlDB = d
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
func NewProposalDB(sqlDB *sql.Database, msh meshDB, logger log.Log) (*DB, error) {
	return newDB(withSQLDB(sqlDB), withMeshDB(msh), withLogger(logger)), nil
}

// Close closes all resources.
func (db *DB) Close() {
}

// HasProposal returns true if the database has the Proposal specified by the ProposalID and false otherwise.
func (db *DB) HasProposal(id types.ProposalID) bool {
	has, _ := proposals.Has(db.sqlDB, id)
	return has
}

// AddProposal adds a proposal to the database.
func (db *DB) AddProposal(ctx context.Context, p *types.Proposal) error {
	if err := db.msh.AddBallot(&p.Ballot); err != nil {
		return fmt.Errorf("proposal add ballot: %w", err)
	}
	if err := db.msh.AddTXsFromProposal(ctx, p.LayerIndex, p.ID(), p.TxIDs); err != nil {
		return fmt.Errorf("proposal add TXs: %w", err)
	}

	if err := proposals.Add(db.sqlDB, p); err != nil {
		return fmt.Errorf("could not add DBProposal %v to database: %w", p.ID(), err)
	}

	db.logger.With().Info("added proposal to database", log.Inline(p))
	return nil
}

// GetProposal retrieves a proposal from the database.
func (db *DB) GetProposal(id types.ProposalID) (*types.Proposal, error) {
	return proposals.Get(db.sqlDB, id)
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
	return proposals.GetIDsByLayer(db.sqlDB, lid)
}

// LayerProposals retrieves all proposals from the layer specified by layer ID.
func (db *DB) LayerProposals(lid types.LayerID) ([]*types.Proposal, error) {
	return proposals.GetByLayer(db.sqlDB, lid)
}
