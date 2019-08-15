package nipst

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/validation"
	"time"
)

func DefaultConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Difficulty = 5
	cfg.NumProvenLabels = 10
	cfg.SpacePerUnit = 1 << 10 // 1KB.
	cfg.FileSize = 1 << 10     // 1KB.
	return *cfg
}

func verifyPost(proof *types.PostProof, space uint64, numProvenLabels uint, difficulty uint) (bool, error) {
	if space%config.LabelGroupSize != 0 {
		return false, fmt.Errorf("space (%d) is not a multiple of LabelGroupSize (%d)", space, config.LabelGroupSize)
	}

	cfg := config.Config{SpacePerUnit: space, NumProvenLabels: uint(numProvenLabels), Difficulty: uint(difficulty)}
	validator := validation.NewValidator(&cfg)
	err := validator.Validate((*proving.Proof)(proof))
	if err != nil {
		return false, err
	}

	return true, nil
}

type PostClient struct {
	id          []byte
	cfg         *config.Config
	initializer *initialization.Initializer
	logger      shared.Logger
}

// A compile time check to ensure that PostClient fully implements PostProverClient.
var _ PostProverClient = (*PostClient)(nil)

func NewPostClient(cfg *config.Config) *PostClient {
	return &PostClient{nil, cfg, nil, shared.DisabledLogger{}}
}

func (c *PostClient) initialize(timeout time.Duration) (commitment *types.PostProof, err error) {
	if c.id == nil {
		return nil, fmt.Errorf("no id provided, cannot initialize post")
	}
	// TODO(moshababo): implement timeout
	//TODO: implement persistence
	if c.cfg.SpacePerUnit%merkle.NodeSize != 0 {
		return nil, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", c.cfg.SpacePerUnit, merkle.NodeSize)
	}
	c.initializer = initialization.NewInitializer(c.cfg, c.id)
	proof, err := c.initializer.Initialize()
	return (*types.PostProof)(proof), err
}

func (c *PostClient) execute(challenge []byte, timeout time.Duration) (*types.PostProof, error) {
	// TODO(moshababo): implement timeout
	if c.id == nil {
		return nil, fmt.Errorf("no id provided, cannot execute post")
	}
	prover := proving.NewProver(c.cfg, c.id)
	proof, err := prover.GenerateProof(challenge)
	return (*types.PostProof)(proof), err
}

func (c *PostClient) Reset() error {
	if c.initializer != nil {
		err := c.initializer.Reset()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *PostClient) SetLogger(logger shared.Logger) {
	c.logger = logger
}

func (c *PostClient) SetParams(id []byte, dataDir string, space uint64) {
	cfg := *c.cfg
	c.cfg = &cfg
	c.cfg.DataDir = dataDir
	c.cfg.SpacePerUnit = space
	c.id = id
	c.initializer = initialization.NewInitializer(c.cfg, c.id)
}

func (c *PostClient) Initialized() bool {
	if c.initializer == nil {
		c.logger.Error("tried to initialize with no id and initializer")
		return false
	}
	state, _, _ := c.initializer.State()
	return state == initialization.StateCompleted
}
