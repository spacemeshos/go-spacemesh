package activation

//func verifyPoST(proof *types.PostProof, id []byte, numLabels uint64, labelSize, k1, k2 uint) error {
//	cfg := config.Config{
//		NumLabels: numLabels,
//		LabelSize: labelSize,
//		K1:        k1,
//		K2:        k2,
//		NumFiles:  1,
//	}
//
//	v, err := validation.NewValidator(&cfg)
//	if err != nil {
//		return err
//	}
//	if err := v.Validate(id, (*proving.Proof)(proof)); err != nil {
//		return err
//	}
//
//	return nil
//}
//
//// PostClient consolidates Proof of Space-Time functionality like initializing space and executing proofs.
//type PostClient struct {
//	minerID     []byte
//	cfg         *config.Config
//	initializer *initialization.Initializer
//	prover      *proving.Prover
//	logger      shared.Logger
//
//	sync.RWMutex
//}
//
//// A compile time check to ensure that PostClient fully implements PostProverClient.
//var _ PostProverClient = (*PostClient)(nil)
//
//// NewPostClient returns a new PostClient based on a configuration and minerID
//func NewPostClient(cfg *config.Config, minerID []byte) (*PostClient, error) {
//	init, err := initialization.NewInitializer(cfg, minerID)
//	if err != nil {
//		return nil, err
//	}
//
//	p, err := proving.NewProver(cfg, minerID)
//	if err != nil {
//		return nil, err
//	}
//
//	return &PostClient{
//		minerID:     minerID,
//		cfg:         cfg,
//		initializer: init,
//		prover:      p,
//		logger:      shared.DisabledLogger{},
//	}, nil
//}
//
//func (c *PostClient) ComputeProviders() []initialization.ComputeProvider {
//	return c.initializer.Providers()
//}
//
//// Initialize is the process in which the prover commits to store some data, by having its storage filled with
//// pseudo-random data with respect to a specific id. This data is the result of a computationally-expensive operation.
//func (c *PostClient) Initialize(computeProviderId uint) error {
//	c.RLock()
//	defer c.RUnlock()
//
//	return c.initializer.Initialize(computeProviderId)
//}
//
//// Execute is the phase in which the prover received a challenge, and proves that his data is still stored (or was
//// recomputed). This phase can be repeated arbitrarily many times without repeating initialization; thus despite the
//// initialization essentially serving as a proof-of-work, the amortized computational complexity can be made arbitrarily
//// small.
//func (c *PostClient) Execute(challenge []byte) (*types.PostProof, error) {
//	c.RLock()
//	defer c.RUnlock()
//
//	proof, err := c.prover.GenerateProof(challenge)
//	return (*types.PostProof)(proof), err
//}
//
//// Reset removes the initialization phase files.
//func (c *PostClient) Reset() error {
//	ok, _, err := c.IsInitialized()
//	if !ok || err != nil {
//		return fmt.Errorf("post not initialized, cannot reset it")
//	}
//
//	c.Lock()
//	defer c.Unlock()
//
//	return c.initializer.Reset()
//}
//
//func (c *PostClient) DiskState() (*initialization.DiskState, error) {
//	c.RLock()
//	defer c.RUnlock()
//
//	return c.initializer.DiskState()
//}
//
//func (c *PostClient) Progress() <-chan float64 {
//	c.RLock()
//	defer c.RUnlock()
//
//	return c.initializer.Progress()
//}
//
//func (c *PostClient) Stop() error {
//	c.RLock()
//	defer c.RUnlock()
//
//	return c.initializer.Stop()
//}
//
//// IsInitialized indicates whether the initialization phase has been completed. If it's not complete the remaining bytes
//// are also returned.
//func (c *PostClient) IsInitialized() (initComplete bool, remainingBytes uint64, err error) {
//	c.RLock()
//	defer c.RUnlock()
//
//	state, err := c.initializer.DiskState()
//	if err != nil {
//		return false, remainingBytes, err
//	}
//	return state.InitState == initialization.InitStateCompleted, 0, nil
//}
//
//// VerifyInitAllowed indicates whether the preconditions for starting the initialization phase are met.
//func (c *PostClient) VerifyInitAllowed() error {
//	c.RLock()
//	defer c.RUnlock()
//
//	return c.initializer.VerifyInitAllowed()
//}
//
//// SetParams updates the datadir and space params in the client config, to be used in the initialization and the
//// execution phases. It overrides the config which the client was instantiated with.
//func (c *PostClient) SetParams(dataDir string, numLabels uint64) error {
//	c.Lock()
//	defer c.Unlock()
//
//	cfg := *c.cfg
//	cfg.DataDir = dataDir
//	cfg.NumLabels = numLabels
//	c.cfg = &cfg
//
//	init, err := initialization.NewInitializer(c.cfg, c.minerID)
//	if err != nil {
//		return err
//	}
//
//	p, err := proving.NewProver(c.cfg, c.minerID)
//	if err != nil {
//		return err
//	}
//
//	init.SetLogger(c.logger)
//	c.initializer = init
//
//	p.SetLogger(c.logger)
//	c.prover = p
//
//	return nil
//}
//
//// SetLogger sets a logger for the client.
//func (c *PostClient) SetLogger(logger shared.Logger) {
//	c.RLock()
//	defer c.RUnlock()
//
//	c.logger = logger
//
//	c.initializer.SetLogger(c.logger)
//	c.prover.SetLogger(c.logger)
//}
//
//// Cfg returns the the client latest config.
//func (c *PostClient) Cfg() *config.Config {
//	c.RLock()
//	defer c.RUnlock()
//
//	cfg := *c.cfg
//	return &cfg
//}
