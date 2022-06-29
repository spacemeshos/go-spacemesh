package cluster

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/spacemeshos/ed25519"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"

	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

var errNotInitialized = errors.New("cluster: not initilized")

const (
	defaultNetID     = 777
	poetSvc          = "poet"
	bootnodesPrefix  = "boot"
	smesherPrefix    = "smesher"
	poetPort         = 80
	defaultBootnodes = 2
)

func setName(podname string) string {
	return podname[:len(podname)-2]
}

func headlessSvc(setname string) string {
	return setname + "-headless"
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

// Reuse will try to recover cluster from the given namespace, if not found
// it will create a new one.
func Reuse(cctx *testcontext.Context, opts ...Opt) (*Cluster, error) {
	cl := New(cctx, opts...)
	if err := cl.reuse(cctx); err != nil {
		if errors.Is(err, errNotInitialized) {
			return Default(cctx, opts...)
		}
		return nil, err
	}
	return cl, nil
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
	persistedFlags bool
	smesherFlags   map[string]DeploymentFlag

	accounts

	bootnodes int
	smeshers  int
	clients   []*NodeClient
	poets     []string
}

func (c *Cluster) persist(ctx *testcontext.Context) error {
	if err := c.accounts.Persist(ctx); err != nil {
		return err
	}
	return c.persistFlags(ctx)
}

func (c *Cluster) persistFlags(ctx *testcontext.Context) error {
	if c.persistedFlags {
		return nil
	}
	ctx.Log.Debugw("persisting flags")
	if err := persistFlags(ctx, c.smesherFlags); err != nil {
		return err
	}
	c.persistedFlags = true
	return nil
}

func (c *Cluster) recoverFlags(ctx *testcontext.Context) error {
	flags, err := recoverFlags(ctx)
	if err != nil {
		return err
	}
	c.smesherFlags[genesisTimeFlag] = flags[genesisTimeFlag]
	c.smesherFlags[accountsFlag] = flags[accountsFlag]
	if !reflect.DeepEqual(c.smesherFlags, flags) {
		return fmt.Errorf("configuration doesn't match %+v != %+v", c.smesherFlags, flags)
	}
	c.persistedFlags = true
	return nil
}

func (c *Cluster) addFlag(flag DeploymentFlag) {
	c.smesherFlags[flag.Name] = flag
}

func (c *Cluster) reuse(cctx *testcontext.Context) error {
	clients, err := discoverNodes(cctx, bootnodesPrefix)
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		return errNotInitialized
	}
	for _, node := range clients {
		cctx.Log.Debugw("discovered existing bootnode", "name", node.Name)
	}
	c.clients = append(c.clients, clients...)
	c.bootnodes = len(clients)

	clients, err = discoverNodes(cctx, smesherPrefix)
	if err != nil {
		return err
	}
	for _, node := range clients {
		cctx.Log.Debugw("discovered existing smesher", "name", node.Name)
	}
	c.clients = append(c.clients, clients...)
	c.smeshers = len(clients)

	cctx.Log.Debugw("discovered cluster", "bootnodes", c.bootnodes, "smeshers", c.smeshers)
	if err := c.accounts.Recover(cctx); err != nil {
		return err
	}
	if err := c.recoverFlags(cctx); err != nil {
		return err
	}
	return nil
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
		gateways = append(gateways, fmt.Sprintf("dns:///%s.%s:9092", bootnode.Name, headlessSvc(setName(bootnode.Name))))
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
	if err := c.persist(cctx); err != nil {
		return err
	}
	flags := []DeploymentFlag{}
	for _, flag := range c.smesherFlags {
		flags = append(flags, flag)
	}
	clients, err := deployNodes(cctx, bootnodesPrefix, c.bootnodes, c.bootnodes+n, flags)
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
	if err := c.persist(cctx); err != nil {
		return err
	}
	flags := []DeploymentFlag{}
	for _, flag := range c.smesherFlags {
		flags = append(flags, flag)
	}
	flags = append(flags, Bootnodes(extractP2PEndpoints(c.clients[:c.bootnodes])...))
	clients, err := deployNodes(cctx, "smesher", c.smeshers, c.smeshers+n, flags)
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

// DeleteSmeshers will remove n smeshers from the end.
func (c *Cluster) DeleteSmeshers(cctx *testcontext.Context, n int) error {
	if n > c.smeshers-c.bootnodes {
		return fmt.Errorf("can't remove bootnodes. max number of removable smeshers is %d", c.smeshers-c.bootnodes)
	}
	clients, err := deleteNodes(cctx, "smesher", c.smeshers, c.smeshers-n)
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
	keys      []*signer
	persisted bool
}

func (a *accounts) Accounts() int {
	return len(a.keys)
}

func (a *accounts) Private(i int) ed25519.PrivateKey {
	return a.keys[i].PK
}

func (a *accounts) Address(i int) []byte {
	return a.keys[i].Address()
}

func (a *accounts) Persist(ctx *testcontext.Context) error {
	if a.persisted {
		return nil
	}
	data := map[string][]byte{}
	for _, key := range a.keys {
		data[hex.EncodeToString(key.Pub)] = key.PK
	}
	cfgmap := corev1.ConfigMap("accounts", ctx.Namespace).
		WithBinaryData(data)
	_, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Apply(ctx, cfgmap, metav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("failed to persist accounts %+v %w", data, err)
	}
	a.persisted = true
	return nil
}

func (a *accounts) Recover(ctx *testcontext.Context) error {
	a.keys = nil
	cfgmap, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Get(ctx, "accounts", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch accounts %w", err)
	}
	for pub, pk := range cfgmap.BinaryData {
		decoded, err := hex.DecodeString(pub)
		if err != nil {
			return fmt.Errorf("failed to decode pub key %s %w", pub, err)
		}
		a.keys = append(a.keys, &signer{PK: pk, Pub: decoded})
	}
	a.persisted = true
	return nil
}

func genGenesis(signers []*signer) (rst map[string]uint64) {
	rst = map[string]uint64{}
	for _, sig := range signers {
		rst[sig.HexAddress()] = 100000000000000000
	}
	return
}

type signer struct {
	Pub ed25519.PublicKey
	PK  ed25519.PrivateKey
}

func (s *signer) Address() []byte {
	address := wallet.Address(s.Pub)
	return address[:]
}

func (s *signer) HexAddress() string {
	return "0x" + hex.EncodeToString(s.Address())
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

func persistFlags(ctx *testcontext.Context, config map[string]DeploymentFlag) error {
	data := map[string]string{}
	for _, flag := range config {
		data[flag.Name] = flag.Value
	}
	cfgmap := corev1.ConfigMap("flags", ctx.Namespace).
		WithData(data)
	_, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Apply(ctx, cfgmap, metav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("failed to persist accounts %+v %w", data, err)
	}
	return nil
}

func recoverFlags(ctx *testcontext.Context) (map[string]DeploymentFlag, error) {
	flags := map[string]DeploymentFlag{}
	cfgmap, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Get(ctx, "flags", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch flags %w", err)
	}
	for name, value := range cfgmap.Data {
		flags[name] = DeploymentFlag{Name: name, Value: value}
	}
	return flags, nil
}
