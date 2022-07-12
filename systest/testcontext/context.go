package testcontext

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	chaosoperatorv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	testid    = flag.String("testid", "", "Name of the pod that runs tests.")
	imageFlag = flag.String("image", "spacemeshos/go-spacemesh-dev:proposal-events",
		"go-spacemesh image")
	poetImage     = flag.String("poet-image", "spacemeshos/poet:ef8f28a", "poet server image")
	namespaceFlag = flag.String("namespace", "",
		"namespace for the cluster. if empty every test will use random namespace")
	logLevel          = zap.LevelFlag("level", zap.InfoLevel, "verbosity of the logger")
	bootstrapDuration = flag.Duration("bootstrap", 30*time.Second,
		"bootstrap time is added to the genesis time. it may take longer on cloud environmens due to the additional resource management")
	clusterSize  = flag.Int("size", 10, "size of the cluster. all test must use at most this number of smeshers")
	testTimeout  = flag.Duration("test-timeout", 60*time.Minute, "timeout for a single test")
	keep         = flag.Bool("keep", false, "if true cluster will not be removed after test is finished")
	clusters     = flag.Int("clusters", 1, "controls how many clusters are deployed on k8s")
	nodeSelector = stringToString{}
	labels       = stringSet{}
	tokens       chan struct{}
	initTokens   sync.Once
)

const (
	keepLabel        = "keep"
	clusterSizeLabel = "size"
)

func init() {
	flag.Var(nodeSelector, "node-selector", "select where test pods will be scheduled")
	flag.Var(labels, "labels", "test will be executed only if it matches all labels")
}

func rngName() string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	const choices = "qwertyuiopasdfghjklzxcvbnm"
	buf := make([]byte, 4)
	for i := range buf {
		buf[i] = choices[rng.Intn(len(choices))]
	}
	return string(buf)
}

// Context must be created for every test that needs isolated cluster.
type Context struct {
	context.Context
	Client            *kubernetes.Clientset
	BootstrapDuration time.Duration
	ClusterSize       int
	Generic           client.Client
	TestID            string
	Keep              bool
	Namespace         string
	Image             string
	PoetImage         string
	NodeSelector      map[string]string
	Log               *zap.SugaredLogger
}

func cleanup(tb testing.TB, f func()) {
	tb.Cleanup(f)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		f()
		os.Exit(1)
	}()
}

func deleteNamespace(ctx *Context) error {
	err := ctx.Client.CoreV1().Namespaces().Delete(ctx, ctx.Namespace, apimetav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete namespace %s: %w", ctx.Namespace, err)
	}
	return nil
}

func deployNamespace(ctx *Context) error {
	_, err := ctx.Client.CoreV1().Namespaces().Apply(ctx, corev1.Namespace(ctx.Namespace).WithLabels(map[string]string{
		"testid":         ctx.TestID,
		keepLabel:        strconv.FormatBool(ctx.Keep),
		clusterSizeLabel: strconv.Itoa(ctx.ClusterSize),
	}),
		apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("create namespace %s: %w", ctx.Namespace, err)
	}
	return nil
}

func updateContext(ctx *Context) error {
	ns, err := ctx.Client.CoreV1().Namespaces().Get(ctx, ctx.Namespace,
		apimetav1.GetOptions{})
	if err != nil || ns == nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	keepval, exists := ns.Labels[keepLabel]
	if !exists {
		ctx.Log.Panic("invalid state. keep label should exist")
	}
	keep, err := strconv.ParseBool(keepval)
	if err != nil {
		ctx.Log.Panicw("invalid state. keep label should be parsable as a boolean",
			"keepval", keepval)
	}
	ctx.Keep = ctx.Keep || keep

	sizeval, _ := ns.Labels[clusterSizeLabel]
	if err != nil {
		ctx.Log.Panic("invalid state. cluster size label should exist")
	}
	size, err := strconv.Atoi(sizeval)
	if err != nil {
		ctx.Log.Panicw("invalid state. size label should be parsable as an integer",
			"sizeval", sizeval)
	}
	ctx.ClusterSize = size
	return nil
}

// Labels sets list of labels for the test.
func Labels(labels ...string) Opt {
	return func(c *cfg) {
		for _, label := range labels {
			c.labels[label] = struct{}{}
		}
	}
}

// SkipClusterLimits will not block if there are no available tokens.
func SkipClusterLimits() Opt {
	return func(c *cfg) {
		c.skipLimits = true
	}
}

// Opt is for configuring Context.
type Opt func(*cfg)

func newCfg() *cfg {
	return &cfg{
		labels: map[string]struct{}{},
	}
}

type cfg struct {
	labels     map[string]struct{}
	skipLimits bool
}

// New creates context for the test.
func New(t *testing.T, opts ...Opt) *Context {
	initTokens.Do(func() {
		tokens = make(chan struct{}, *clusters)
	})

	c := newCfg()
	for _, opt := range opts {
		opt(c)
	}
	for label := range labels {
		if _, exist := c.labels[label]; !exist {
			t.Skipf("not labeled with '%s'", label)
		}
	}
	if !c.skipLimits {
		tokens <- struct{}{}
		t.Cleanup(func() { <-tokens })
	}
	config, err := rest.InClusterConfig()
	require.NoError(t, err)

	clientset, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	ns := *namespaceFlag
	if len(ns) == 0 {
		ns = "test-" + rngName()
	}
	scheme := runtime.NewScheme()
	require.NoError(t, chaosoperatorv1alpha1.AddToScheme(scheme))

	generic, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), *testTimeout)
	t.Cleanup(cancel)
	cctx := &Context{
		Context:           ctx,
		Namespace:         ns,
		BootstrapDuration: *bootstrapDuration,
		Client:            clientset,
		Generic:           generic,
		TestID:            *testid,
		Keep:              *keep,
		ClusterSize:       *clusterSize,
		Image:             *imageFlag,
		PoetImage:         *poetImage,
		NodeSelector:      nodeSelector,
		Log:               zaptest.NewLogger(t, zaptest.Level(logLevel)).Sugar(),
	}
	err = updateContext(cctx)
	require.NoError(t, err)
	if !cctx.Keep {
		cleanup(t, func() {
			if err := deleteNamespace(cctx); err != nil {
				cctx.Log.Errorf("cleanup failed", "error", err)
				return
			}
			cctx.Log.Infow("namespace was deleted", "namespace", cctx.Namespace)
		})
	}
	require.NoError(t, deployNamespace(cctx))
	cctx.Log.Infow("using", "namespace", cctx.Namespace)
	return cctx
}
