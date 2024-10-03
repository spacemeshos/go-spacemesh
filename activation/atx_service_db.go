package activation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

// dbAtxService implements AtxService by accessing the state database.
type dbAtxService struct {
	golden    types.ATXID
	logger    *zap.Logger
	db        sql.Executor
	atxsdata  *atxsdata.Data
	validator nipostValidator
	cfg       dbAtxServiceConfig
}

type dbAtxServiceConfig struct {
	// delay before PoST in ATX is considered valid (counting from the time it was received)
	postValidityDelay time.Duration
	trusted           []types.NodeID
}

type dbAtxServiceOption func(*dbAtxServiceConfig)

func WithPostValidityDelay(delay time.Duration) dbAtxServiceOption {
	return func(cfg *dbAtxServiceConfig) {
		cfg.postValidityDelay = delay
	}
}

func WithTrustedIDs(ids ...types.NodeID) dbAtxServiceOption {
	return func(cfg *dbAtxServiceConfig) {
		cfg.trusted = ids
	}
}

func NewDBAtxService(
	db sql.Executor,
	golden types.ATXID,
	atxsdata *atxsdata.Data,
	validator nipostValidator,
	logger *zap.Logger,
	opts ...dbAtxServiceOption,
) *dbAtxService {
	cfg := dbAtxServiceConfig{
		postValidityDelay: time.Hour * 12,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &dbAtxService{
		golden:    golden,
		logger:    logger,
		db:        db,
		atxsdata:  atxsdata,
		validator: validator,
		cfg:       cfg,
	}
}

func (s *dbAtxService) Atx(_ context.Context, id types.ATXID) (*types.ActivationTx, error) {
	return atxs.Get(s.db, id)
}

func (s *dbAtxService) LastATX(ctx context.Context, id types.NodeID) (*types.ActivationTx, error) {
	atxid, err := atxs.GetLastIDByNodeID(s.db, id)
	if err != nil {
		return nil, fmt.Errorf("getting last ATXID: %w", err)
	}
	return atxs.Get(s.db, atxid)
}

func (s *dbAtxService) PositioningATX(ctx context.Context, maxPublish types.EpochID) (types.ATXID, error) {
	latestPublished, err := atxs.LatestEpoch(s.db)
	if err != nil {
		return types.EmptyATXID, fmt.Errorf("get latest epoch: %w", err)
	}
	s.logger.Info("searching for positioning atx", zap.Uint32("latest_epoch", latestPublished.Uint32()))

	// positioning ATX publish epoch must be lower than the publish epoch of built ATX
	positioningAtxPublished := min(latestPublished, maxPublish)
	id, err := findFullyValidHighTickAtx(
		ctx,
		s.atxsdata,
		positioningAtxPublished,
		s.golden,
		s.validator, s.logger,
		VerifyChainOpts.AssumeValidBefore(time.Now().Add(-s.cfg.postValidityDelay)),
		VerifyChainOpts.WithTrustedIDs(s.cfg.trusted...),
		VerifyChainOpts.WithLogger(s.logger),
	)
	if err != nil {
		s.logger.Info("search failed - using golden atx as positioning atx", zap.Error(err))
		id = s.golden
	}

	return id, nil
}

func findFullyValidHighTickAtx(
	ctx context.Context,
	atxdata *atxsdata.Data,
	publish types.EpochID,
	goldenATXID types.ATXID,
	validator nipostValidator,
	logger *zap.Logger,
	opts ...VerifyChainOption,
) (types.ATXID, error) {
	var found *types.ATXID

	// iterate trough epochs, to get first valid, not malicious ATX with the biggest height
	atxdata.IterateHighTicksInEpoch(publish+1, func(id types.ATXID) (contSearch bool) {
		logger.Debug("found candidate for high-tick atx", log.ZShortStringer("id", id))
		if ctx.Err() != nil {
			return false
		}
		// verify ATX-candidate by getting their dependencies (previous Atx, positioning ATX etc.)
		// and verifying PoST for every dependency
		if err := validator.VerifyChain(ctx, id, goldenATXID, opts...); err != nil {
			logger.Debug("rejecting candidate for high-tick atx", zap.Error(err), log.ZShortStringer("id", id))
			return true
		}
		found = &id
		return false
	})

	if ctx.Err() != nil {
		return types.ATXID{}, ctx.Err()
	}

	if found == nil {
		return types.ATXID{}, ErrNotFound
	}

	return *found, nil
}
