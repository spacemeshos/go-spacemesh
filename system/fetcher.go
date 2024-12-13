package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/fetcher.go -source=./fetcher.go

// Fetcher is a general interface that defines a component capable of fetching data from remote peers.
type Fetcher interface {
	AtxFetcher
	BlockFetcher
	PoetProofFetcher
	BallotFetcher
	ActiveSetFetcher
	ProposalFetcher
	TxFetcher
	PeerTracker
}

// BlockFetcher defines an interface for fetching blocks from remote peers.
type BlockFetcher interface {
	GetBlocks(context.Context, []types.BlockID) error
}

type GetAtxOpts struct {
	LimitingOff bool
	Callback    func(types.ATXID, error)
}

type GetAtxOpt func(*GetAtxOpts)

// WithoutLimiting disables rate limiting when downloading ATXs.
func WithoutLimiting() GetAtxOpt {
	return func(opts *GetAtxOpts) {
		opts.LimitingOff = true
	}
}

// WithATXCallback sets a callback function to be called after each ATX is downloaded,
// found locally or failed to download.
// The callback is guaranteed to be called exactly once for each ATX ID passed to GetAtxs.
// The callback is guaranteed not to be invoked after GetAtxs returns.
// The callback may be called concurrently from multiple goroutines.
// A non-nil error is passed in case the ATX cannot be found locally and failed to download.
func WithATXCallback(callback func(types.ATXID, error)) GetAtxOpt {
	return func(opts *GetAtxOpts) {
		opts.Callback = callback
	}
}

// AtxFetcher defines an interface for fetching ATXs from remote peers.
type AtxFetcher interface {
	GetAtxs(context.Context, []types.ATXID, ...GetAtxOpt) error
}

// TxFetcher defines an interface for fetching transactions from remote peers.
type TxFetcher interface {
	GetBlockTxs(context.Context, []types.TransactionID) error
	GetProposalTxs(context.Context, []types.TransactionID) error
}

// PoetProofFetcher defines an interface for fetching PoET proofs from remote peers.
type PoetProofFetcher interface {
	GetPoetProof(context.Context, types.Hash32) error
}

// BallotFetcher defines an interface for fetching Ballot from remote peers.
type BallotFetcher interface {
	GetBallots(context.Context, []types.BallotID) error
}

// ProposalFetcher defines an interface for fetching Proposal from remote peers.
type ProposalFetcher interface {
	GetProposals(context.Context, []types.ProposalID) error
}

// ActiveSetFetcher defines an interface downloading active set.
type ActiveSetFetcher interface {
	GetActiveSet(context.Context, types.Hash32) error
}

// PeerTracker defines an interface to track peer hashes.
type PeerTracker interface {
	RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32)
}
