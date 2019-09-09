package nipst

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/validation"
	"sync"
)

func DefaultConfig() config.Config {
	return *config.DefaultConfig()
}

func verifyPost(proof *types.PostProof, space uint64, numProvenLabels uint, difficulty uint) error {
	if space%config.LabelGroupSize != 0 {
		return fmt.Errorf("space (%d) is not a multiple of LabelGroupSize (%d)", space, config.LabelGroupSize)
	}

	cfg := config.Config{
		SpacePerUnit:    space,
		NumProvenLabels: uint(numProvenLabels),
		Difficulty:      uint(difficulty),
		NumFiles:        1,
	}

	v, err := validation.NewValidator(&cfg)
	if err != nil {
		return err
	}
	if err := v.Validate((*proving.Proof)(proof)); err != nil {
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

	sync.RWMutex
}

// A compile time check to ensure that PostClient fully implements PostProverClient.
var _ PostProverClient = (*PostClient)(nil)

func NewPostClient(cfg *config.Config, id []byte) (*PostClient, error) {
	init, err := initialization.NewInitializer(cfg, id)
	if err != nil {
		return nil, err
	}

	p, err := proving.NewProver(cfg, id)
	if err != nil {
		return nil, err
	}

	return &PostClient{
		id:          id,
		cfg:         cfg,
		initializer: init,
		prover:      p,
		logger:      shared.DisabledLogger{},
	}, nil
}

func (c *PostClient) Initialize() (commitment *types.PostProof, err error) {
	c.RLock()
	defer c.RUnlock()

	proof, err := c.initializer.Initialize()
	return (*types.PostProof)(proof), err
}

func (c *PostClient) Execute(challenge []byte) (*types.PostProof, error) {
	c.RLock()
	defer c.RUnlock()

	proof, err := c.prover.GenerateProof(challenge)
	return (*types.PostProof)(proof), err
}

func (c *PostClient) Reset() error {
	c.Lock()
	defer c.Unlock()

	return c.initializer.Reset()
}

func (c *PostClient) IsInitialized() (bool, error) {
	c.RLock()
	defer c.RUnlock()

	state, _, err := c.initializer.State()
	if err != nil {
		return false, err
	}

	return state == initialization.StateCompleted, nil
}

func (c *PostClient) VerifyInitAllowed() error {
	c.RLock()
	defer c.RUnlock()

	return c.initializer.VerifyInitAllowed()
}

func (c *PostClient) SetParams(dataDir string, space uint64) error {
	c.Lock()
	defer c.Unlock()

	cfg := *c.cfg
	cfg.DataDir = dataDir
	cfg.SpacePerUnit = space
	c.cfg = &cfg

	init, err := initialization.NewInitializer(c.cfg, c.id)
	if err != nil {
		return err
	}

	p, err := proving.NewProver(c.cfg, c.id)
	if err != nil {
		return err
	}

	init.SetLogger(c.logger)
	c.initializer = init

	p.SetLogger(c.logger)
	c.prover = p

	return nil
}

func (c *PostClient) SetLogger(logger shared.Logger) {
	c.RLock()
	defer c.RUnlock()

	c.logger = logger

	c.initializer.SetLogger(c.logger)
	c.prover.SetLogger(c.logger)
}

func (c *PostClient) Cfg() *config.Config {
	c.RLock()
	defer c.RUnlock()

	cfg := *c.cfg
	return &cfg
}
