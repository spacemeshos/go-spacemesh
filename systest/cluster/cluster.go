package cluster

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/ed25519"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/emptypb"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

var errNotInitialized = errors.New("cluster: not initialized")

const (
	defaultExtraData = "systest"
	poetSvc          = "poet"
	bootnodesPrefix  = "boot"
	smesherPrefix    = "smesher"
	poetPort         = 80
	defaultBootnodes = 2

	poetFlags    = "poetflags"
	smesherFlags = "smesherflags"
)

func setName(podname string) string {
	return podname[:len(podname)-2]
}

func headlessSvc(setname string) string {
	return setname + "-headless"
}

// MakePoetEndpoint generate a poet endpoint for the ith instance.
func MakePoetEndpoint(ith int) string {
	return fmt.Sprintf("%s:%d", createPoetIdentifier(ith), poetPort)
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
	if err := cl.AddPoets(cctx); err != nil {
		return nil, err
	}
	if err := cl.AddSmeshers(cctx, cctx.ClusterSize-defaultBootnodes); err != nil {
		return nil, err
	}
	return cl, nil
}

// New initializes Cluster with options.
func New(cctx *testcontext.Context, opts ...Opt) *Cluster {
	cluster := &Cluster{
		smesherFlags: map[string]DeploymentFlag{},
		poetFlags:    map[string]DeploymentFlag{},
	}
	genesis := GenesisTime(time.Now().Add(cctx.BootstrapDuration))
	cluster.addFlag(genesis)
	cluster.addFlag(GenesisExtraData(defaultExtraData))
	cluster.addFlag(TargetOutbound(defaultTargetOutbound(cctx.ClusterSize)))

	cluster.addPoetFlag(genesis)
	cluster.addPoetFlag(PoetRestListen(poetPort))

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
	persisted    bool
	smesherFlags map[string]DeploymentFlag
	poetFlags    map[string]DeploymentFlag

	accounts

	bootnodes int
	smeshers  int
	clients   []*NodeClient
	poets     []*NodeClient
}

// GenesisID computes id from the configuration.
func (c *Cluster) GenesisID() types.Hash20 {
	return types.Hash32(hash.Sum(
		[]byte(c.smesherFlags[genesisTimeFlag].Value),
		[]byte(c.smesherFlags[genesisExtraData].Value),
	)).ToHash20()
}

func (c *Cluster) nextSmesher() int {
	if c.smeshers <= c.bootnodes {
		return 0
	}
	return decodeOrdinal(c.clients[len(c.clients)-1].Name) + 1
}

func (c *Cluster) persist(ctx *testcontext.Context) error {
	if c.persisted {
		return nil
	}
	if err := c.accounts.Persist(ctx); err != nil {
		return err
	}
	if err := c.persistFlags(ctx); err != nil {
		return err
	}
	if err := c.persistConfigs(ctx); err != nil {
		return err
	}
	c.persisted = true
	return nil
}

func (c *Cluster) persistConfigs(ctx *testcontext.Context) error {
	_, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Apply(
		ctx,
		corev1.ConfigMap(spacemeshConfigMapName, ctx.Namespace).WithData(map[string]string{
			attachedSmesherConfig: smesherConfig.Get(ctx.Parameters),
		}),
		apimetav1.ApplyOptions{FieldManager: "test"},
	)
	if err != nil {
		return fmt.Errorf("apply cfgmap %v/%v: %w", ctx.Namespace, spacemeshConfigMapName, err)
	}
	_, err = ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Apply(
		ctx,
		corev1.ConfigMap(poetConfigMapName, ctx.Namespace).WithData(map[string]string{
			attachedPoetConfig: poetConfig.Get(ctx.Parameters),
		}),
		apimetav1.ApplyOptions{FieldManager: "test"},
	)
	if err != nil {
		return fmt.Errorf("apply cfgmap %v/%v: %w", ctx.Namespace, poetConfigMapName, err)
	}
	return nil
}

func (c *Cluster) persistFlags(ctx *testcontext.Context) error {
	ctx.Log.Debugf("persisting flags %+v", c.smesherFlags)
	if err := persistFlags(ctx, smesherFlags, c.smesherFlags); err != nil {
		return err
	}
	if err := persistFlags(ctx, poetFlags, c.poetFlags); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) recoverFlags(ctx *testcontext.Context) error {
	sflags, err := recoverFlags(ctx, smesherFlags)
	if err != nil {
		return err
	}
	c.smesherFlags[genesisTimeFlag] = sflags[genesisTimeFlag]
	c.smesherFlags[accountsFlag] = sflags[accountsFlag]
	if !reflect.DeepEqual(c.smesherFlags, sflags) {
		return fmt.Errorf("sm configuration doesn't match %+v != %+v", c.smesherFlags, sflags)
	}
	pflags, err := recoverFlags(ctx, poetFlags)
	if err != nil {
		return err
	}
	c.poetFlags[genesisTimeFlag] = pflags[genesisTimeFlag]
	if !reflect.DeepEqual(c.poetFlags, pflags) {
		return fmt.Errorf("poet configuration doesn't match %+v != %+v", c.poetFlags, sflags)
	}
	c.persisted = true
	return nil
}

func (c *Cluster) addFlag(flag DeploymentFlag) {
	c.smesherFlags[flag.Name] = flag
}

func (c *Cluster) addPoetFlag(flag DeploymentFlag) {
	c.poetFlags[flag.Name] = flag
}

func (c *Cluster) reuse(cctx *testcontext.Context) error {
	clients, err := discoverNodes(cctx, bootnodesPrefix, Smesher)
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

	clients, err = discoverNodes(cctx, smesherPrefix, Smesher)
	if err != nil {
		return err
	}
	for _, node := range clients {
		cctx.Log.Debugw("discovered existing smesher", "name", node.Name)
	}
	c.clients = append(c.clients, clients...)
	c.smeshers = len(clients)

	c.poets, err = discoverNodes(cctx, poetSvc, Poet)
	if err != nil {
		return err
	}
	for _, poet := range c.poets {
		cctx.Log.Debugw("discovered existing poets", "name", poet.Name)
	}

	cctx.Log.Debugw("discovered cluster", "bootnodes", c.bootnodes, "smeshers", c.smeshers, "poets", len(c.poets))
	if err := c.accounts.Recover(cctx); err != nil {
		return err
	}
	if err := c.recoverFlags(cctx); err != nil {
		return err
	}
	return nil
}

// AddPoets spawns poets up to configured number of poets.
func (c *Cluster) AddPoets(cctx *testcontext.Context) error {
	for i := 0; i < cctx.PoetSize; i++ {
		c.AddPoet(cctx)
	}
	return nil
}

func (c *Cluster) firstFreePoetId() int {
	set := make(map[int]struct{}, c.Poets())
	for _, poet := range c.poets {
		set[decodePoetIdentifier(poet.Identifier)] = struct{}{}
	}
	for id := 0; ; id += 1 {
		if _, ok := set[id]; !ok {
			return id
		}
	}
}

// AddPoet spawns a single poet with the first available id.
// Id is of form "poet-N", where N ∈ [0, ∞).
func (c *Cluster) AddPoet(cctx *testcontext.Context) error {
	if c.bootnodes == 0 {
		return fmt.Errorf("bootnodes are used as a gateways. create atleast one before adding a poet server")
	}
	if err := c.persist(cctx); err != nil {
		return err
	}
	var flags []DeploymentFlag
	for _, flag := range c.poetFlags {
		flags = append(flags, flag)
	}
	for _, bootnode := range c.clients[:c.bootnodes] {
		flags = append(flags, Gateway(fmt.Sprintf("dns:///%s.%s:9092", bootnode.Name, headlessSvc(setName(bootnode.Name)))))
	}

	id := createPoetIdentifier(c.firstFreePoetId())
	cctx.Log.Debugw("deploying poet", "id", id)
	pod, err := deployPoet(cctx, id, flags...)
	if err != nil {
		return err
	}
	c.poets = append(c.poets, pod)
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
func (c *Cluster) AddSmeshers(tctx *testcontext.Context, n int) error {
	if err := c.resourceControl(tctx, n); err != nil {
		return err
	}
	if err := c.persist(tctx); err != nil {
		return err
	}
	flags := []DeploymentFlag{}
	for _, flag := range c.smesherFlags {
		flags = append(flags, flag)
	}
	endpoints, err := extractP2PEndpoints(tctx, c.clients[:c.bootnodes])
	if err != nil {
		return fmt.Errorf("extracing p2p endpoints %w", err)
	}
	flags = append(flags, Bootnodes(endpoints...))
	clients, err := deployNodes(tctx, smesherPrefix, c.nextSmesher(), c.nextSmesher()+n, flags)
	if err != nil {
		return err
	}
	bootnodes := c.clients[:c.bootnodes]
	smeshers := c.clients[c.bootnodes:]
	c.clients = nil
	c.clients = append(c.clients, bootnodes...)
	c.clients = append(c.clients, smeshers...)
	c.clients = append(c.clients, clients...)
	c.smeshers += len(clients)
	return nil
}

// DeletePoets delete all poet servers.
func (c *Cluster) DeletePoets(cctx *testcontext.Context) error {
	for {
		if c.Poets() == 0 {
			return nil
		}
		if err := c.DeletePoet(cctx, c.Poets()-1); err != nil {
			return err
		}
	}
}

func (c *Cluster) DeletePoet(cctx *testcontext.Context, i int) error {
	poet := c.Poet(i)
	if poet == nil {
		return nil
	}
	if err := deletePoet(cctx, poet.Identifier); err != nil {
		return err
	}
	c.poets = append(c.poets[0:i], c.poets[i+1:]...)

	return nil
}

// DeleteSmesher will smesher i from the cluster.
func (c *Cluster) DeleteSmesher(cctx *testcontext.Context, node *NodeClient) error {
	err := deleteNode(cctx, node.Name)
	if err != nil {
		return err
	}

	clients := c.clients
	c.clients = nil
	for _, n := range clients {
		if n.Name == node.Name {
			continue
		}
		c.clients = append(c.clients, n)
	}
	c.smeshers = len(c.clients)
	return nil
}

// Bootnodes returns number of the bootnodes in the cluster.
func (c *Cluster) Bootnodes() int {
	return c.bootnodes
}

// Total returns total number of clients.
func (c *Cluster) Total() int {
	return len(c.clients)
}

// Poets returns total number of poet servers.
func (c *Cluster) Poets() int {
	return len(c.poets)
}

// Poet returns client for i-th poet node.
func (c *Cluster) Poet(i int) *NodeClient {
	return c.poets[i]
}

// Client returns client for i-th node, either bootnode or smesher.
func (c *Cluster) Client(i int) *NodeClient {
	return c.clients[i]
}

// CloseClients closes connections to clients.
func (c *Cluster) CloseClients() error {
	var eg errgroup.Group
	for _, client := range c.clients {
		eg.Go(client.Close)
	}
	return eg.Wait()
}

// Wait for i-th client to be up.
func (c *Cluster) Wait(tctx *testcontext.Context, i int) error {
	nc, err := waitNode(tctx, c.Client(i).Name, Smesher)
	if err != nil {
		return err
	}
	c.clients[i] = nc
	return nil
}

// Account contains address and private key.
type Account struct {
	PrivateKey ed25519.PrivateKey
	Address    types.Address
}

func (a Account) String() string {
	return a.Address.String()
}

type accounts struct {
	keys      []*signer
	persisted bool
}

func (a *accounts) Account(i int) Account {
	return Account{
		PrivateKey: a.Private(i),
		Address:    a.Address(i),
	}
}

func (a *accounts) Accounts() int {
	return len(a.keys)
}

func (a *accounts) Private(i int) ed25519.PrivateKey {
	return a.keys[i].PK
}

func (a *accounts) Address(i int) types.Address {
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
	_, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Apply(ctx, cfgmap, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("failed to persist accounts %+v %w", data, err)
	}
	a.persisted = true
	return nil
}

func (a *accounts) Recover(ctx *testcontext.Context) error {
	a.keys = nil
	cfgmap, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Get(ctx, "accounts", apimetav1.GetOptions{})
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
		rst[sig.Address().String()] = 100000000000000000
	}
	return
}

type signer struct {
	Pub ed25519.PublicKey
	PK  ed25519.PrivateKey
}

func (s *signer) Address() types.Address {
	return wallet.Address(s.Pub)
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

func extractP2PEndpoints(tctx *testcontext.Context, nodes []*NodeClient) ([]string, error) {
	var (
		rst          = make([]string, len(nodes))
		rctx, cancel = context.WithTimeout(tctx, 10*time.Second)
		eg, ctx      = errgroup.WithContext(rctx)
	)
	defer cancel()
	for i := range nodes {
		i := i
		n := nodes[i]
		eg.Go(func() error {
			dbg := pb.NewDebugServiceClient(n)
			info, err := dbg.NetworkInfo(ctx, &emptypb.Empty{})
			if err != nil {
				return err
			}
			rst[i] = p2pEndpoint(n.Node, info.Id)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return rst, nil
}

func defaultTargetOutbound(size int) int {
	if size < 10 {
		return 3
	}
	return int(0.3 * float64(size))
}

func persistFlags(ctx *testcontext.Context, name string, config map[string]DeploymentFlag) error {
	data := map[string]string{}
	for _, flag := range config {
		data[flag.Name] = flag.Value
	}
	cfgmap := corev1.ConfigMap(name, ctx.Namespace).
		WithData(data)
	_, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Apply(ctx, cfgmap, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("failed to persist accounts %+v %w", data, err)
	}
	return nil
}

func recoverFlags(ctx *testcontext.Context, name string) (map[string]DeploymentFlag, error) {
	flags := map[string]DeploymentFlag{}
	cfgmap, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Get(ctx, name, apimetav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch flags %w", err)
	}
	for name, value := range cfgmap.Data {
		flags[name] = DeploymentFlag{Name: name, Value: value}
	}
	return flags, nil
}
