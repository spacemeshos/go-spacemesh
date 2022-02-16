package cluster

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

const (
	defaultNetID     = 777
	poetSvc          = "poet"
	bootnodesPrefix  = "boot"
	poetPort         = 80
	defaultBootnodes = 2
)

func headlessSvc(name string) string {
	return name + "-headless"
}

func poetEndpoint() string {
	return fmt.Sprintf("%s:%d", poetSvc, poetPort)
}

// Opt is for configuring cluster.
type Opt func(c *Cluster)

// WithSmesherFlag adds smesher flag.
func WithSmesherFlag(flag DeploymentFlag) Opt {
	return func(c *Cluster) {
		c.addFlag(flag)
	}
}

// WithKeys generates n prefunded keys.
func WithKeys(n int) Opt {
	return func(c *Cluster) {
		c.accounts = accounts{keys: genSigners(n)}
	}
}

// Default deployes bootnodes, one poet and the smeshers according to the cluster size.
func Default(cctx *testcontext.Context, opts ...Opt) (*Cluster, error) {
	cl := New(cctx, opts...)
	if err := cl.AddBootnodes(cctx, defaultBootnodes); err != nil {
		return nil, err
	}
	if err := cl.AddPoet(cctx); err != nil {
		return nil, err
	}
	if err := cl.AddSmeshers(cctx, cctx.ClusterSize-defaultBootnodes); err != nil {
		return nil, err
	}
	return cl, nil
}

// New initializes Cluster with options.
func New(cctx *testcontext.Context, opts ...Opt) *Cluster {
	cluster := &Cluster{smesherFlags: map[string]DeploymentFlag{}}
	cluster.addFlag(GenesisTime(time.Now().Add(cctx.BootstrapDuration)))
	cluster.addFlag(TargetOutbound(defaultTargetOutbound(cctx.ClusterSize)))
	cluster.addFlag(NetworkID(defaultNetID))
	cluster.addFlag(PoetEndpoint(poetEndpoint()))
	for _, opt := range opts {
		opt(cluster)
	}
	if len(cluster.keys) > 0 {
		cluster.addFlag(Accounts(genGenesis(cluster.keys)))
	}
	return cluster
}

// Cluster for managing state of the spacemesh cluster.
type Cluster struct {
	smesherFlags map[string]DeploymentFlag

	accounts

	bootnodes int
	smeshers  int
	clients   []*NodeClient
	poets     []string
}

func (c *Cluster) addFlag(flag DeploymentFlag) {
	c.smesherFlags[flag.Name] = flag
}

// AddPoet ...
func (c *Cluster) AddPoet(cctx *testcontext.Context) error {
	if c.bootnodes == 0 {
		return fmt.Errorf("bootnodes are used as a gateways. create atleast one before adding a poet server")
	}
	if len(c.poets) == 1 {
		return fmt.Errorf("only one poet is supported")
	}
	gateways := []string{}
	for _, bootnode := range c.clients[:c.bootnodes] {
		gateways = append(gateways, fmt.Sprintf("dns:///%s.%s:9092", bootnode.Name, headlessSvc(bootnodesPrefix)))
	}
	endpoint, err := deployPoet(cctx, gateways...)
	if err != nil {
		return err
	}
	c.poets = append(c.poets, endpoint)
	return nil
}

func (c *Cluster) resourceControl(cctx *testcontext.Context, n int) error {
	if len(c.clients)+n > cctx.ClusterSize {
		// maybe account for poet as well?
		return fmt.Errorf("max cluster size is %v", cctx.ClusterSize)
	}
	return nil
}

// AddBootnodes ...
func (c *Cluster) AddBootnodes(cctx *testcontext.Context, n int) error {
	if err := c.resourceControl(cctx, n); err != nil {
		return err
	}
	flags := []DeploymentFlag{}
	for _, flag := range c.smesherFlags {
		flags = append(flags, flag)
	}
	clients, err := deployNodes(cctx, bootnodesPrefix, c.bootnodes+n, flags)
	if err != nil {
		return err
	}
	smeshers := c.clients[c.bootnodes:]
	c.clients = nil
	c.clients = append(c.clients, clients...)
	c.clients = append(c.clients, smeshers...)
	c.bootnodes = len(clients)
	return nil
}

// AddSmeshers ...
func (c *Cluster) AddSmeshers(cctx *testcontext.Context, n int) error {
	if err := c.resourceControl(cctx, n); err != nil {
		return err
	}
	flags := []DeploymentFlag{}
	for _, flag := range c.smesherFlags {
		flags = append(flags, flag)
	}
	flags = append(flags, Bootnodes(extractP2PEndpoints(c.clients[:c.bootnodes])...))
	clients, err := deployNodes(cctx, "smesher", c.smeshers+n, flags)
	if err != nil {
		return err
	}
	bootnodes := c.clients[:c.bootnodes]
	c.clients = nil
	c.clients = append(c.clients, bootnodes...)
	c.clients = append(c.clients, clients...)
	c.smeshers = len(clients)
	return nil
}

// Total returns total number of clients.
func (c *Cluster) Total() int {
	return len(c.clients)
}

// Client returns client for i-th node, either bootnode or smesher.
func (c *Cluster) Client(i int) *NodeClient {
	return c.clients[i]
}

// Wait for i-th client to be up.
func (c *Cluster) Wait(tctx *testcontext.Context, i int) error {
	nc, err := waitSmesher(tctx, c.Client(i).Name)
	if err != nil {
		return err
	}
	c.clients[i] = nc
	return nil
}

type accounts struct {
	keys []*signer
}

func (a *accounts) Private(i int) ed25519.PrivateKey {
	return a.keys[i].PK
}

func (a *accounts) Address(i int) string {
	return a.keys[i].Address()
}

func genGenesis(signers []*signer) (rst map[string]uint64) {
	rst = map[string]uint64{}
	for _, sig := range signers {
		rst[sig.Address()] = 100000000000000000
	}
	return
}

type signer struct {
	Pub ed25519.PublicKey
	PK  ed25519.PrivateKey
}

func (s *signer) Address() string {
	encoded := hex.EncodeToString(s.Pub[12:])
	return "0x" + encoded
}

func genSigners(n int) (rst []*signer) {
	for i := 0; i < n; i++ {
		rst = append(rst, genSigner())
	}
	return
}

func genSigner() *signer {
	pub, pk, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	return &signer{Pub: pub, PK: pk}
}

func extractNames(nodes []*NodeClient) []string {
	var rst []string
	for _, n := range nodes {
		rst = append(rst, n.Name)
	}
	return rst
}

func extractP2PEndpoints(nodes []*NodeClient) []string {
	var rst []string
	for _, n := range nodes {
		rst = append(rst, n.P2PEndpoint())
	}
	return rst
}

func defaultTargetOutbound(size int) int {
	if size < 10 {
		return 3
	}
	return int(0.3 * float64(size))
}
