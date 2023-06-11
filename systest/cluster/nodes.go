package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/spacemeshos/go-spacemesh/systest/parameters"
	"github.com/spacemeshos/go-spacemesh/systest/parameters/fastnet"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

var (
	poetConfig = parameters.String(
		"poet",
		"configuration for poet service",
		fastnet.PoetConfig,
	)
	smesherConfig = parameters.String(
		"smesher",
		"configuration for smesher service",
		fastnet.SmesherConfig,
	)

	smesherResources = parameters.NewParameter(
		"smesher_resources",
		"requests and limits for smesher container",
		&apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse("0.2"),
				apiv1.ResourceMemory: resource.MustParse("400Mi"),
			},
			Limits: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse("2"),
				apiv1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		toResources,
	)
	bootstrapperResources = parameters.NewParameter(
		"bootstrapper_resources",
		"requests and limits for bootstrapper container",
		&apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse("0.1"),
				apiv1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse("0.1"),
				apiv1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
		toResources,
	)
	poetResources = parameters.NewParameter(
		"poet_resources",
		"requests and limits for poet container",
		&apiv1.ResourceRequirements{
			Requests: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse("0.5"),
				apiv1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: apiv1.ResourceList{
				apiv1.ResourceCPU:    resource.MustParse("0.5"),
				apiv1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		toResources,
	)
)

func toResources(value string) (*apiv1.ResourceRequirements, error) {
	var rst apiv1.ResourceRequirements
	if err := json.Unmarshal([]byte(value), &rst); err != nil {
		return nil, fmt.Errorf("unmarshaling %s into apiv1.ResourceRequirements: %w", value, err)
	}
	return &rst, nil
}

const (
	configDir = "/etc/config/"

	attachedPoetConfig    = "poet.conf"
	attachedSmesherConfig = "smesher.json"

	poetConfigMapName      = "poet"
	spacemeshConfigMapName = "spacemesh"

	// smeshers are splitted in 10 approximately equal buckets
	// to enable running chaos mesh tasks on the different parts of the cluster.
	buckets = 10
)

const (
	prometheusScrapePort = 9216
	phlareScrapePort     = 6060
)

// Node ...
type Node struct {
	Name      string
	P2P, GRPC uint16
}

// P2PEndpoint returns full p2p endpoint, including identity.
func p2pEndpoint(n Node, ip, id string) string {
	return fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip, n.P2P, id)
}

// NodeClient is a Node with attached grpc connection.
type NodeClient struct {
	session *testcontext.Context
	Node

	mu   sync.Mutex
	conn *grpc.ClientConn
}

func (n *NodeClient) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resetConn(n.conn)
}

func (n *NodeClient) Resolve(ctx context.Context) (string, error) {
	pod, err := waitPod(n.session, n.Name)
	if err != nil {
		return "", err
	}
	return pod.Status.PodIP, nil
}

func (n *NodeClient) ensureConn(ctx context.Context) (*grpc.ClientConn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.conn != nil {
		return n.conn, nil
	}
	pod, err := waitPod(n.session, n.Name)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.DialContext(ctx, fmt.Sprintf("%s:%d", pod.Status.PodIP, n.GRPC),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	n.conn = conn
	return n.conn, nil
}

func (n *NodeClient) resetConn(conn *grpc.ClientConn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.conn != nil && n.conn == conn {
		n.conn.Close()
		n.conn = nil
	}
}

func (n *NodeClient) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	conn, err := n.ensureConn(ctx)
	if err != nil {
		return err
	}
	err = conn.Invoke(ctx, method, args, reply, opts...)
	if err != nil {
		s, _ := status.FromError(err)
		if s.Code() != codes.InvalidArgument && s.Code() != codes.Canceled {
			// check for app error. this is not exhaustive.
			// the goal is to reset connection if pods were redeployed and changed IP
			n.resetConn(conn)
		}
	}
	return err
}

func (n *NodeClient) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	conn, err := n.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := n.conn.NewStream(ctx, desc, method, opts...)
	if err != nil {
		n.resetConn(conn)
	}
	return stream, err
}

func deployPoetD(ctx *testcontext.Context, id string, flags ...DeploymentFlag) (*NodeClient, error) {
	args := []string{
		"-c=" + configDir + attachedPoetConfig,
	}
	for _, flag := range flags {
		args = append(args, flag.Flag())
	}

	ctx.Log.Debugw("deploying poet pod", "id", id, "args", args, "image", ctx.PoetImage)

	labels := nodeLabels(poetApp, id)
	deployment := appsv1.Deployment(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(appsv1.DeploymentSpec().
			WithSelector(metav1.LabelSelector().WithMatchLabels(labels)).
			WithReplicas(1).
			WithTemplate(corev1.PodTemplateSpec().
				WithLabels(labels).
				WithSpec(corev1.PodSpec().
					WithNodeSelector(ctx.NodeSelector).
					WithVolumes(corev1.Volume().
						WithName("config").
						WithConfigMap(corev1.ConfigMapVolumeSource().WithName(poetConfigMapName)),
					).
					WithContainers(corev1.Container().
						WithName("poet").
						WithImage(ctx.PoetImage).
						WithArgs(args...).
						WithPorts(
							corev1.ContainerPort().WithName("rest").WithProtocol("TCP").WithContainerPort(poetPort),
						).
						WithVolumeMounts(
							corev1.VolumeMount().WithName("config").WithMountPath(configDir),
						).
						WithResources(corev1.ResourceRequirements().
							WithRequests(poetResources.Get(ctx.Parameters).Requests).
							WithLimits(poetResources.Get(ctx.Parameters).Limits),
						),
					),
				)))

	_, err := ctx.Client.AppsV1().Deployments(ctx.Namespace).Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return nil, fmt.Errorf("create poet: %w", err)
	}
	return &NodeClient{
		session: ctx,
		Node: Node{
			Name: id,
		},
	}, nil
}

func deployBootnodeSvc(ctx *testcontext.Context, id string) error {
	labels := nodeLabels(bootnodeApp, id)
	svc := corev1.Service(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("grpc").WithPort(9092).WithProtocol("TCP"),
				corev1.ServicePort().WithName("p2p").WithPort(7513).WithProtocol("TCP"),
			).
			WithClusterIP("None"),
		)
	_, err := ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("apply headless service: %w", err)
	}
	return nil
}

func deployPoetSvc(ctx *testcontext.Context, id string) (*apiv1.Service, error) {
	ctx.Log.Debugw("deploying poet service", "id", id)
	labels := nodeLabels(poetApp, id)
	svc := corev1.Service(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("rest").WithPort(poetPort).WithProtocol("TCP"),
			),
		)

	return ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
}

func createPoetIdentifier(id int) string {
	return fmt.Sprintf("%s-%d", poetApp, id)
}

func createBootstrapperIdentifier(id int) string {
	return fmt.Sprintf("%s-%d", bootstrapperApp, id)
}

func decodePoetIdentifier(id string) int {
	parts := strings.Split(id, "-")
	if len(parts) != 2 {
		panic(fmt.Sprintf("unexpected name format %s", id))
	}
	ord, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}
	return ord
}

// deployPoet creates a poet Pod and exposes it via a Service.
// Flags are passed to the poet Pod as arguments.
func deployPoet(ctx *testcontext.Context, id string, flags ...DeploymentFlag) (*NodeClient, error) {
	if _, err := deployPoetSvc(ctx, id); err != nil {
		return nil, fmt.Errorf("apply poet service: %w", err)
	}

	node, err := deployPoetD(ctx, id, flags...)
	if err != nil {
		return nil, err
	}

	return node, nil
}

func deleteServiceAndPod(ctx *testcontext.Context, id string) error {
	errPod := ctx.Client.AppsV1().Deployments(ctx.Namespace).DeleteCollection(ctx, apimetav1.DeleteOptions{}, apimetav1.ListOptions{LabelSelector: labelSelector(id)})
	var errSvc error
	if svcs, err := ctx.Client.CoreV1().Services(ctx.Namespace).List(ctx, apimetav1.ListOptions{LabelSelector: labelSelector(id)}); err == nil {
		for _, svc := range svcs.Items {
			err = ctx.Client.CoreV1().Services(ctx.Namespace).Delete(ctx, svc.ObjectMeta.Name, apimetav1.DeleteOptions{})
			if errSvc == nil {
				errSvc = err
			}
		}
	}
	if errPod != nil {
		return errPod
	}
	return errSvc
}

// areContainersReady checks if all containers are ready in pod.
func areContainersReady(pod *apiv1.Pod) bool {
	for _, c := range pod.Status.ContainerStatuses {
		if !c.Ready {
			return false
		}
	}
	return true
}

func waitPod(ctx *testcontext.Context, id string) (*apiv1.Pod, error) {
	watcher, err := ctx.Client.CoreV1().Pods(ctx.Namespace).Watch(ctx, apimetav1.ListOptions{
		LabelSelector: labelSelector(id),
	})
	if err != nil {
		return nil, err
	}
	defer watcher.Stop()
	for {
		var pod *apiv1.Pod
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, open := <-watcher.ResultChan():
			if !open {
				return nil, fmt.Errorf("watcher is terminated while waiting for pod with id %v", id)
			}
			pod = ev.Object.(*apiv1.Pod)
			if pod.Status.Phase == apiv1.PodRunning && areContainersReady(pod) {
				return pod, nil
			}
		}
	}
}

func nodeLabels(name string, id string) map[string]string {
	return map[string]string{
		// app identifies resource kind (Node, Poet).
		// It can be used to select all Pods of given kind.
		"app": name,
		// id uniquely identifies a resource (i.e. poet-0).
		"id": id,
	}
}

func labelSelector(id string) string {
	return fmt.Sprintf("id=%s", id)
}

func deployNodes(ctx *testcontext.Context, kind string, from, to int, flags []DeploymentFlag) ([]*NodeClient, error) {
	var (
		eg      errgroup.Group
		clients = make(chan *NodeClient, to-from)
	)
	for i := from; i < to; i++ {
		i := i
		finalFlags := make([]DeploymentFlag, len(flags), len(flags)+ctx.PoetSize)
		copy(finalFlags, flags)
		for idx := 0; idx < ctx.PoetSize; idx++ {
			finalFlags = append(finalFlags, PoetEndpoint(MakePoetEndpoint(idx)))
		}
		if ctx.BootstrapperSize > 1 {
			finalFlags = append(finalFlags, BootstrapperUrl(BootstrapperEndpoint(i%ctx.BootstrapperSize)))
		} else {
			finalFlags = append(finalFlags, BootstrapperUrl(BootstrapperEndpoint(0)))
		}
		eg.Go(func() error {
			id := fmt.Sprintf("%s-%d", kind, i)
			labels := nodeLabels(kind, id)
			labels["bucket"] = strconv.Itoa(i % buckets)
			if err := deployNode(ctx, id, labels, finalFlags); err != nil {
				return err
			}
			clients <- &NodeClient{
				session: ctx,
				Node: Node{
					Name: id,
					P2P:  7513,
					GRPC: 9092,
				},
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(clients)
	var rst []*NodeClient
	for node := range clients {
		rst = append(rst, node)
	}
	sort.Slice(rst, func(i, j int) bool {
		return decodeOrdinal(rst[i].Name) < decodeOrdinal(rst[j].Name)
	})
	return rst, nil
}

func deleteNode(ctx *testcontext.Context, id string) error {
	if err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Delete(ctx, id, apimetav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting configmap %s/%s: %w", ctx.Namespace, id, err)
	}
	if err := ctx.Client.AppsV1().Deployments(ctx.Namespace).
		Delete(ctx, id, apimetav1.DeleteOptions{}); err != nil {
		return err
	}
	return nil
}

func deployNode(ctx *testcontext.Context, id string, labels map[string]string, flags []DeploymentFlag) error {
	cmd := []string{
		"/bin/go-spacemesh",
		"-c=" + configDir + attachedSmesherConfig,
		"--pprof-server",
		"--smeshing-opts-datadir=/data/post",
		"-d=/data/state",
		"--log-encoder=json",
		"--metrics",
		"--metrics-port=" + strconv.Itoa(prometheusScrapePort),
	}
	for _, flag := range flags {
		cmd = append(cmd, flag.Flag())
	}
	deployment := appsv1.Deployment(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(appsv1.DeploymentSpec().
			WithSelector(metav1.LabelSelector().WithMatchLabels(labels)).
			WithReplicas(1).
			WithTemplate(corev1.PodTemplateSpec().
				WithLabels(labels).
				WithAnnotations(
					map[string]string{
						"prometheus.io/port":   strconv.Itoa(prometheusScrapePort),
						"prometheus.io/scrape": "true",
					},
				).
				WithSpec(corev1.PodSpec().
					WithNodeSelector(ctx.NodeSelector).
					WithVolumes(
						corev1.Volume().WithName("config").
							WithConfigMap(corev1.ConfigMapVolumeSource().WithName(spacemeshConfigMapName)),
						corev1.Volume().WithName("data").
							WithEmptyDir(corev1.EmptyDirVolumeSource().
								WithSizeLimit(resource.MustParse(ctx.Storage.Size))),
					).
					WithDNSConfig(corev1.PodDNSConfig().WithOptions(
						corev1.PodDNSConfigOption().WithName("timeout").WithValue("1"),
						corev1.PodDNSConfigOption().WithName("attempts").WithValue("5"),
					)).
					WithContainers(corev1.Container().
						WithName("smesher").
						WithImage(ctx.Image).
						WithImagePullPolicy(apiv1.PullIfNotPresent).
						WithPorts(
							corev1.ContainerPort().WithContainerPort(7513).WithName("p2p"),
							corev1.ContainerPort().WithContainerPort(9092).WithName("grpc"),
							corev1.ContainerPort().WithContainerPort(prometheusScrapePort).WithName("prometheus"),
							corev1.ContainerPort().WithContainerPort(phlareScrapePort).WithName("pprof"),
						).
						WithVolumeMounts(
							corev1.VolumeMount().WithName("data").WithMountPath("/data"),
							corev1.VolumeMount().WithName("config").WithMountPath(configDir),
						).
						WithResources(corev1.ResourceRequirements().
							WithRequests(smesherResources.Get(ctx.Parameters).Requests).
							WithLimits(smesherResources.Get(ctx.Parameters).Limits),
						).
						WithStartupProbe(
							corev1.Probe().WithTCPSocket(
								corev1.TCPSocketAction().WithPort(intstr.FromInt(9092)),
							).WithInitialDelaySeconds(10).WithPeriodSeconds(10),
						).
						WithEnv(
							corev1.EnvVar().WithName("GOMAXPROCS").WithValue("4"),
						).
						WithCommand(cmd...),
					),
				)))
	_, err := ctx.Client.AppsV1().Deployments(ctx.Namespace).
		Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("apply pod %s: %w", id, err)
	}
	if strings.Contains(id, bootnodeApp) {
		return deployBootnodeSvc(ctx, id)
	}
	return nil
}

func deployBootstrapper(ctx *testcontext.Context, id string, bsEpochs []int, flags ...DeploymentFlag) (*NodeClient, error) {
	if _, err := deployBootstrapperSvc(ctx, id); err != nil {
		return nil, fmt.Errorf("apply poet service: %w", err)
	}

	node, err := deployBootstrapperD(ctx, id, bsEpochs, flags...)
	if err != nil {
		return nil, err
	}

	return node, nil
}

func deployBootstrapperSvc(ctx *testcontext.Context, id string) (*apiv1.Service, error) {
	ctx.Log.Debugw("deploying bootstrapper service", "id", id)
	labels := nodeLabels(bootstrapperApp, id)
	svc := corev1.Service(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("rest").WithProtocol("TCP").WithPort(bootstrapperPort),
			),
		)

	return ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
}

func commaSeparatedList(epochs []int) string {
	var b strings.Builder
	for i, e := range epochs {
		b.WriteString(strconv.Itoa(e))
		if i < len(epochs)-1 {
			b.WriteString(",")
		}
	}
	return b.String()
}

func deployBootstrapperD(ctx *testcontext.Context, id string, bsEpochs []int, flags ...DeploymentFlag) (*NodeClient, error) {
	cmd := []string{
		"/bin/go-bootstrapper",
		commaSeparatedList(bsEpochs),
		"--serve-update",
		"--data-dir=/data/bootstrapper",
		"--epoch-offset=1",
		"--port=" + strconv.Itoa(bootstrapperPort),
		// empty so it generates local random beacon instead of making http queries to bitcoin explorer
		"--bitcoin-endpoint=",
	}
	for _, flag := range flags {
		cmd = append(cmd, flag.Flag())
	}

	ctx.Log.Debugw("deploying bootstrapper pod", "id", id, "cmd", cmd, "image", ctx.BootstrapperImage)

	labels := nodeLabels(bootstrapperApp, id)
	deployment := appsv1.Deployment(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(appsv1.DeploymentSpec().
			WithSelector(metav1.LabelSelector().WithMatchLabels(labels)).
			WithReplicas(1).
			WithTemplate(corev1.PodTemplateSpec().
				WithLabels(labels).
				WithSpec(corev1.PodSpec().
					WithNodeSelector(ctx.NodeSelector).
					WithContainers(corev1.Container().
						WithName("bootstrapper").
						WithImage(ctx.BootstrapperImage).
						WithPorts(
							corev1.ContainerPort().WithName("rest").WithProtocol("TCP").WithContainerPort(bootstrapperPort),
						).
						WithResources(corev1.ResourceRequirements().
							WithRequests(bootstrapperResources.Get(ctx.Parameters).Requests).
							WithLimits(bootstrapperResources.Get(ctx.Parameters).Limits),
						).
						WithCommand(cmd...),
					),
				)))

	_, err := ctx.Client.AppsV1().Deployments(ctx.Namespace).Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return nil, fmt.Errorf("create bootstrapper: %w", err)
	}
	return &NodeClient{
		session: ctx,
		Node: Node{
			Name: id,
		},
	}, nil
}

// DeploymentFlag allows to configure specific flags for application binaries.
type DeploymentFlag struct {
	Name, Value string
}

// Flag returns parseable flag from Name and Value.
func (d DeploymentFlag) Flag() string {
	return d.Name + "=" + d.Value
}

// PoetEndpoint flag.
func PoetEndpoint(endpoint string) DeploymentFlag {
	return DeploymentFlag{Name: "--poet-server", Value: endpoint}
}

// MinPeers flag.
func MinPeers(target int) DeploymentFlag {
	return DeploymentFlag{Name: "--min-peers", Value: strconv.Itoa(target)}
}

func BootstrapperUrl(endpoint string) DeploymentFlag {
	return DeploymentFlag{Name: "--bootstrap-url", Value: endpoint}
}

const (
	genesisTimeFlag  = "--genesis-time"
	genesisExtraData = "--genesis-extra-data"
	accountsFlag     = "--accounts"
)

// GenesisTime flag.
func GenesisTime(t time.Time) DeploymentFlag {
	return DeploymentFlag{Name: genesisTimeFlag, Value: t.Format(time.RFC3339)}
}

// GenesisExtraData flag.
func GenesisExtraData(extra string) DeploymentFlag {
	return DeploymentFlag{Name: genesisExtraData, Value: extra}
}

// Bootnodes flag.
func Bootnodes(bootnodes ...string) DeploymentFlag {
	return DeploymentFlag{Name: "--bootnodes", Value: strings.Join(bootnodes, ",")}
}

// Accounts flag.
func Accounts(accounts map[string]uint64) DeploymentFlag {
	var parts []string
	for name, value := range accounts {
		parts = append(parts, fmt.Sprintf("%s=%d", name, value))
	}
	return DeploymentFlag{Name: "--accounts", Value: strings.Join(parts, ",")}
}

// DurationFlag is a generic duration flag.
func DurationFlag(flag string, d time.Duration) DeploymentFlag {
	return DeploymentFlag{Name: flag, Value: d.String()}
}

// PoetRestListen socket pair with http api.
func PoetRestListen(port int) DeploymentFlag {
	return DeploymentFlag{Name: "--restlisten", Value: fmt.Sprintf("0.0.0.0:%d", port)}
}

func StartSmeshing(start bool) DeploymentFlag {
	return DeploymentFlag{Name: "--smeshing-start", Value: fmt.Sprintf("%v", start)}
}

func GenerateFallback() DeploymentFlag {
	return DeploymentFlag{Name: "--fallback", Value: fmt.Sprintf("%v", true)}
}
