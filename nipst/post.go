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
	cfg    *config.Config
	logger shared.Logger
}

// A compile time check to ensure that PostClient fully implements PostProverClient.
var _ PostProverClient = (*PostClient)(nil)

func NewPostClient(cfg *config.Config) *PostClient {
	return &PostClient{cfg, shared.DisabledLogger{}}
}

func (c *PostClient) initialize(id []byte, timeout time.Duration) (commitment *types.PostProof, err error) {
	// TODO(moshababo): implement timeout
	//TODO: implement persistence
	if c.cfg.SpacePerUnit%merkle.NodeSize != 0 {
		return nil, fmt.Errorf("space (%d) is not a multiple of merkle.NodeSize (%d)", c.cfg.SpacePerUnit, merkle.NodeSize)
	}

	proof, err := initialization.NewInitializer(c.cfg, c.logger).Initialize(id)
	return (*types.PostProof)(proof), err
}

func (c *PostClient) execute(id []byte, challenge []byte, timeout time.Duration) (*types.PostProof, error) {
	// TODO(moshababo): implement timeout
	prover := proving.NewProver(c.cfg, c.logger)
	proof, err := prover.GenerateProof(id, challenge)
	return (*types.PostProof)(proof), err
}

func (c *PostClient) SetLogger(logger shared.Logger) {
	c.logger = logger
}

func (c *PostClient) SetPostParams(logicalDrive string, commitmentSize uint64) {
	cfg := *c.cfg
	c.cfg = &cfg
	c.cfg.DataDir = logicalDrive
	c.cfg.SpacePerUnit = commitmentSize
}
