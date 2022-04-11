package proposals

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
)

// DB holds all data for proposals.
type DB struct {
	logger log.Log
	sqlDB  *sql.Database
}

// dbOpt for configuring DB.
type dbOpt func(*DB)

func withLogger(l log.Log) dbOpt {
	return func(db *DB) {
		db.logger = l
	}
}

func newDB(sqlDB *sql.Database, opts ...dbOpt) *DB {
	db := &DB{
		logger: log.NewNop(),
		sqlDB:  sqlDB,
	}
	for _, opt := range opts {
		opt(db)
	}
	return db
}

// NewProposalDB returns a new DB for proposals.
func NewProposalDB(sqlDB *sql.Database, logger log.Log) (*DB, error) {
	return newDB(sqlDB, withLogger(logger)), nil
}

// HasProposal returns true if the database has the Proposal specified by the ProposalID and false otherwise.
func (db *DB) HasProposal(id types.ProposalID) bool {
	has, _ := proposals.Has(db.sqlDB, id)
	return has
}

// AddProposal adds a proposal to the database.
func (db *DB) AddProposal(ctx context.Context, p *types.Proposal) error {
	if err := proposals.Add(db.sqlDB, p); err != nil {
		return fmt.Errorf("could not add DBProposal %v to database: %w", p.ID(), err)
	}

	db.logger.WithContext(ctx).With().Info("added proposal to database", log.Inline(p))
	return nil
}

// DelProposal dels a proposal from the database
func (db *DB) DelProposal(ctx context.Context, id types.ProposalID) error {
	if err := proposals.Del(db.sqlDB, id); err != nil {
		return fmt.Errorf("could not remove Proposal %v from database: %w", id, err)
	}
	db.logger.WithContext(ctx).With().Info("removed proposal from database", id)
	return nil
}

// GetProposal retrieves a proposal from the database.
func (db *DB) GetProposal(id types.ProposalID) (*types.Proposal, error) {
	return proposals.Get(db.sqlDB, id)
}

// GetProposals retrieves multiple proposals from the database.
func (db *DB) GetProposals(pids []types.ProposalID) ([]*types.Proposal, error) {
	result := make([]*types.Proposal, 0, len(pids))
	var (
		p   *types.Proposal
		err error
	)
	for _, pid := range pids {
		if p, err = db.GetProposal(pid); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

// Get Proposal encoded in byte using ProposalID hash.
func (db *DB) Get(hash []byte) ([]byte, error) {
	return proposals.GetBlob(db.sqlDB, hash)
}

// LayerProposalIDs retrieves all proposal IDs from the layer specified by layer ID.
func (db *DB) LayerProposalIDs(lid types.LayerID) ([]types.ProposalID, error) {
	return proposals.GetIDsByLayer(db.sqlDB, lid)
}

// LayerProposals retrieves all proposals from the layer specified by layer ID.
func (db *DB) LayerProposals(lid types.LayerID) ([]*types.Proposal, error) {
	return proposals.GetByLayer(db.sqlDB, lid)
}
