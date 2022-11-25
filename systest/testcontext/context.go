package testcontext

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	chaos "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
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

	"github.com/spacemeshos/go-spacemesh/systest/parameters"
)

var (
	configname  = flag.String("configname", "", "config map name. if not empty parameters will be loaded from specified configmap")
	clusters    = flag.Int("clusters", 1, "controls tests parallelization by creating multiple spacemesh clusters at the same time")
	logLevel    = zap.LevelFlag("level", zap.InfoLevel, "verbosity of the logger")
	TestTimeout = flag.Duration("test-timeout", 60*time.Minute, "timeout for a single test")
	labels      = stringSet{}

	tokens     chan struct{}
	initTokens sync.Once
)

func init() {
	flag.Var(labels, "labels", "test will be executed only if it matches all labels")
}

var (
	testid = parameters.String(
		"testid", "Name of the pod that runs tests.", "",
	)
	imageFlag = parameters.String(
		"image",
		"go-spacemesh image",
		"spacemeshos/go-spacemesh-dev:proposal-events",
	)
	poetImage = parameters.String(
		"poet-image",
		"spacemeshos/poet:87608eda8307b44984c191afc65cdbcec0d8d1c4",
		"poet server image",
	)
	namespaceFlag = parameters.String(
		"namespace",
		"namespace for the cluster. if empty every test will use random namespace",
		"",
	)
	bootstrapDuration = parameters.Duration(
		"bootstrap-duration",
		"bootstrap time is added to the genesis time. it may take longer on cloud environmens due to the additional resource management",
		30*time.Second,
	)
	clusterSize = parameters.Int(
		"cluster-size",
		"size of the cluster. all test must use at most this number of smeshers",
		10,
	)
	poetSize = parameters.Int(
		"poet-size", "size of the poet servers", 1,
	)
	storage = parameters.String(
		"storage", "<class>=<size> for the storage", "standard=1Gi",
	)
	keep = parameters.Bool(
		"keep", "if true cluster will not be removed after test is finished",
	)
	nodeSelector = parameters.NewParameter(
		"node-selector", "select where test pods will be scheduled",
		stringToString{},
		func(value string) (stringToString, error) {
			rst := stringToString{}
			if err := rst.Set(value); err != nil {
				return nil, err
			}
			return rst, nil
		},
	)
)

const nsfile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

const (
	keepLabel        = "keep"
	clusterSizeLabel = "size"
	poetSizeLabel    = "poet-size"
)

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
	Client            *kubernetes.Clientset
	Parameters        *parameters.Parameters
	BootstrapDuration time.Duration
	ClusterSize       int
	PoetSize          int
	Generic           client.Client
	TestID            string
	Keep              bool
	Namespace         string
	Image             string
	PoetImage         string
	Storage           struct {
		Size  string
		Class string
	}
	NodeSelector map[string]string
	Log          *zap.SugaredLogger
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

func deleteNamespace(ctx context.Context, tctx *Context) error {
	err := tctx.Client.CoreV1().Namespaces().Delete(ctx, tctx.Namespace, apimetav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete namespace %s: %w", tctx.Namespace, err)
	}
	return nil
}

func deployNamespace(ctx context.Context, tctx *Context) error {
	_, err := tctx.Client.CoreV1().Namespaces().Apply(ctx,
		corev1.Namespace(tctx.Namespace).WithLabels(map[string]string{
			"testid":         tctx.TestID,
			keepLabel:        strconv.FormatBool(tctx.Keep),
			clusterSizeLabel: strconv.Itoa(tctx.ClusterSize),
			poetSizeLabel:    strconv.Itoa(tctx.PoetSize),
		}),
		apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("create namespace %s: %w", tctx.Namespace, err)
	}
	return nil
}

func getStorage(p *parameters.Parameters) (string, string, error) {
	value := storage.Get(p)
	parts := strings.Split(value, "=")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("malformed storage %s. see default value for example", value)
	}
	return parts[0], parts[1], nil
}

func updateContext(ctx context.Context, tctx *Context) error {
	ns, err := tctx.Client.CoreV1().Namespaces().Get(ctx, tctx.Namespace,
		apimetav1.GetOptions{})
	if err != nil || ns == nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	keepval, exists := ns.Labels[keepLabel]
	if !exists {
		tctx.Log.Panic("invalid state. keep label should exist")
	}
	keep, err := strconv.ParseBool(keepval)
	if err != nil {
		tctx.Log.Panicw("invalid state. keep label should be parsable as a boolean",
			"keepval", keepval)
	}
	tctx.Keep = tctx.Keep || keep

	sizeval := ns.Labels[clusterSizeLabel]
	if err != nil {
		tctx.Log.Panic("invalid state. cluster size label should exist")
	}
	size, err := strconv.Atoi(sizeval)
	if err != nil {
		tctx.Log.Panicw("invalid state. size label should be parsable as an integer",
			"sizeval", sizeval)
	}
	tctx.ClusterSize = size

	psizeval := ns.Labels[poetSizeLabel]
	if err != nil {
		tctx.Log.Panic("invalid state. poet size label should exist")
	}
	psize, err := strconv.Atoi(psizeval)
	if err != nil {
		tctx.Log.Panicw("invalid state. poet size label should be parsable as an integer",
			"psizeval", psizeval)
	}
	tctx.PoetSize = psize
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
func New(ctx context.Context, t *testing.T, opts ...Opt) *Context {
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

	scheme := runtime.NewScheme()
	require.NoError(t, chaos.AddToScheme(scheme))

	generic, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	podns, err := os.ReadFile(nsfile)
	require.NoError(t, err, "reading nsfile at %s", nsfile)
	paramsData, err := clientset.CoreV1().ConfigMaps(string(podns)).Get(ctx, *configname, apimetav1.GetOptions{})
	require.NoError(t, err, "get cfgmap %s/%s", string(podns), *configname)

	p := parameters.FromValues(paramsData.Data)

	class, size, err := getStorage(p)
	require.NoError(t, err)

	ns := namespaceFlag.Get(p)
	if len(ns) == 0 {
		ns = "test-" + rngName()
	}
	tctx := &Context{
		Parameters:        p,
		Namespace:         ns,
		BootstrapDuration: bootstrapDuration.Get(p),
		Client:            clientset,
		Generic:           generic,
		TestID:            testid.Get(p),
		Keep:              keep.Get(p),
		ClusterSize:       clusterSize.Get(p),
		PoetSize:          poetSize.Get(p),
		Image:             imageFlag.Get(p),
		PoetImage:         poetImage.Get(p),
		NodeSelector:      nodeSelector.Get(p),
		Log:               zaptest.NewLogger(t, zaptest.Level(logLevel)).Sugar(),
	}
	tctx.Storage.Class = class
	tctx.Storage.Size = size
	err = updateContext(ctx, tctx)
	require.NoError(t, err)
	if !tctx.Keep {
		cleanup(t, func() {
			if err := deleteNamespace(ctx, tctx); err != nil {
				tctx.Log.Errorw("cleanup failed", "error", err)
				return
			}
			tctx.Log.Infow("namespace was deleted", "namespace", tctx.Namespace)
		})
	}
	require.NoError(t, deployNamespace(ctx, tctx))
	tctx.Log.Infow("using", "namespace", tctx.Namespace)
	return tctx
}
