package types

import "github.com/spacemeshos/post/initialization"

// PostSetupComputeProvider represent a compute provider for Post setup data creation.
type PostSetupComputeProvider initialization.ComputeProvider

// PostConfig is the configuration of the Post protocol, used for data creation, proofs generation and validation.
type PostConfig struct {
	MinNumUnits   uint32 `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint32 `mapstructure:"post-max-numunits"`
	BitsPerLabel  uint8  `mapstructure:"post-bits-per-label"`
	LabelsPerUnit uint64 `mapstructure:"post-labels-per-unit"`
	K1            uint32 `mapstructure:"post-k1"`
	K2            uint32 `mapstructure:"post-k2"`
}

// PostSetupOpts are the options used to initiate a Post setup data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostSetupOpts struct {
	DataDir           string `mapstructure:"smeshing-opts-datadir"`
	NumUnits          uint32 `mapstructure:"smeshing-opts-numunits"`
	NumFiles          uint32 `mapstructure:"smeshing-opts-numfiles"`
	ComputeProviderID int    `mapstructure:"smeshing-opts-provider"`
	Throttle          bool   `mapstructure:"smeshing-opts-throttle"`
}

// PostSetupStatus represents a status snapshot of the Post setup.
type PostSetupStatus struct {
	State            PostSetupState
	NumLabelsWritten uint64
	LastOpts         *PostSetupOpts
}

type PostSetupState int32

const (
	PostSetupStateNotStarted PostSetupState = 1 + iota
	PostSetupStateInProgress
	PostSetupStateComplete
	PostSetupStateError
)
