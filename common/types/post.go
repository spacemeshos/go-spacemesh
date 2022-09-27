package types

// PostConfig is the configuration of the Post protocol, used for data creation, proofs generation and validation.
type PostConfig struct {
	BitsPerLabel  uint `mapstructure:"post-bits-per-label"`
	LabelsPerUnit uint `mapstructure:"post-labels-per-unit"`
	MinNumUnits   uint `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint `mapstructure:"post-max-numunits"`
	K1            uint `mapstructure:"post-k1"`
	K2            uint `mapstructure:"post-k2"`
}

// PostSetupOpts are the options used to initiate a Post setup data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostSetupOpts struct {
	DataDir           string `mapstructure:"smeshing-opts-datadir"`
	NumUnits          uint   `mapstructure:"smeshing-opts-numunits"`
	NumFiles          uint   `mapstructure:"smeshing-opts-numfiles"`
	ComputeProviderID int    `mapstructure:"smeshing-opts-provider"`
	Throttle          bool   `mapstructure:"smeshing-opts-throttle"`
}

// PostSetupStatus represents a status snapshot of the Post setup.
type PostSetupStatus struct {
	State            PostSetupState
	NumLabelsWritten uint64
	LastOpts         *PostSetupOpts
	LastError        error
}

type PostSetupState int32
