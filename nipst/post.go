package nipst

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/validation"
)

func DefaultConfig() config.Config {
	return *config.DefaultConfig()
}

func verifyPost(proof *types.PostProof, space uint64, numProvenLabels uint, difficulty uint) error {
	if space%config.LabelGroupSize != 0 {
		return fmt.Errorf("space (%d) is not a multiple of LabelGroupSize (%d)", space, config.LabelGroupSize)
	}

	cfg := config.Config{SpacePerUnit: space, NumProvenLabels: uint(numProvenLabels), Difficulty: uint(difficulty)}
	validator := validation.NewValidator(&cfg)
	if err := validator.Validate((*proving.Proof)(proof)); err != nil {
		return err
	}

	return nil
}

type PostClient struct {
	id          []byte
	cfg         *config.Config
	initializer *initialization.Initializer
	prover      *proving.Prover
	logger      shared.Logger
}

// A compile time check to ensure that PostClient fully implements PostProverClient.
var _ PostProverClient = (*PostClient)(nil)

func NewPostClient(cfg *config.Config, id []byte) *PostClient {
	return &PostClient{
		id:          id,
		cfg:         cfg,
		initializer: initialization.NewInitializer(cfg, id),
		prover:      proving.NewProver(cfg, id),
		logger:      shared.DisabledLogger{},
	}
}

func (c *PostClient) Initialize() (commitment *types.PostProof, err error) {
	proof, err := c.initializer.Initialize()
	return (*types.PostProof)(proof), err
}

func (c *PostClient) Execute(challenge []byte) (*types.PostProof, error) {
	proof, err := c.prover.GenerateProof(challenge)
	return (*types.PostProof)(proof), err
}

func (c *PostClient) Reset() error {
	return c.initializer.Reset()
}

func (c *PostClient) IsInitialized() (bool, error) {
	state, _, err := c.initializer.State()
	if err != nil {
		return false, err
	}

	return state == initialization.StateCompleted, nil
}

func (c *PostClient) VerifyInitAllowed() error {
	return c.initializer.VerifyInitAllowed()
}

func (c *PostClient) SetParams(dataDir string, space uint64) {
	cfg := *c.cfg
	cfg.DataDir = dataDir
	cfg.SpacePerUnit = space

	c.initializer = initialization.NewInitializer(&cfg, c.id)
	c.prover = proving.NewProver(&cfg, c.id)
	c.cfg = &cfg
}

func (c *PostClient) SetLogger(logger shared.Logger) {
	c.logger = logger
}

func (c *PostClient) Cfg() *config.Config {
	cfg := *c.cfg
	return &cfg
}
