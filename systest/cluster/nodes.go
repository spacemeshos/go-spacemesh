package cluster

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/node"
	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/systest/parameters"
	"github.com/spacemeshos/go-spacemesh/systest/parameters/fastnet"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

var (
	certifierConfig = parameters.String(
		"certifier",
		"configuration for certifier service",
		fastnet.CertifierConfig,
	)
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
				apiv1.ResourceCPU:    resource.MustParse("0.4"),
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

	attachedCertifierConfig = "certifier.yaml"
	attachedPoetConfig      = "poet.conf"
	attachedSmesherConfig   = "smesher.json"

	certifierConfigMapName = "certifier"
	poetConfigMapName      = "poet"
	spacemeshConfigMapName = "spacemesh"

	// smeshers are split in 10 approximately equal buckets
	// to enable running chaos mesh tasks on the different parts of the cluster.
	buckets = 10
)

const (
	prometheusScrapePort = 9216
	phlareScrapePort     = 6060
)

// Node ...
type Node struct {
	Name                     string
	P2P, GRPC_PUB, GRPC_PRIV uint16
}

// P2PEndpoint returns full p2p endpoint, including identity.
func p2pEndpoint(n Node, ip, id string) string {
	return fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip, n.P2P, id)
}

// NodeClient is a Node with attached grpc connection.
type NodeClient struct {
	session *testcontext.Context
	Node

	mu       sync.Mutex
	pubConn  *grpc.ClientConn
	privConn *grpc.ClientConn
}

func (n *NodeClient) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resetPubConn(n.pubConn)
	n.resetPrivConn(n.privConn)
}

func (n *NodeClient) Resolve(ctx context.Context) (string, error) {
	pod, err := waitPod(n.session, n.Name)
	if err != nil {
		return "", err
	}
	return pod.Status.PodIP, nil
}

func (n *NodeClient) ensurePubConn(ctx context.Context) (*grpc.ClientConn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pubConn != nil {
		return n.pubConn, nil
	}
	pod, err := waitPod(n.session, n.Name)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(
		fmt.Sprintf("%s:%d", pod.Status.PodIP, n.GRPC_PUB),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	if err := n.waitForConnectionReady(context.Background(), conn); err != nil {
		return nil, err
	}
	n.pubConn = conn
	return n.pubConn, nil
}

// waitForConnectionReady blocks until the connection is ready. It returns an error if the context is canceled or
// timed out.
//
// This doesn't guarantee that the connection will stay ready, but it makes it so that the test runner waits at least
// until the nodes are started before querying them.
func (n *NodeClient) waitForConnectionReady(ctx context.Context, conn *grpc.ClientConn) error {
	// A blocking dial blocks until the clientConn is ready.
	for {
		s := conn.GetState()
		if s == connectivity.Ready {
			return nil
		}
		if s == connectivity.Idle {
			conn.Connect()
		}
		if !conn.WaitForStateChange(ctx, s) {
			// ctx got timeout or canceled.
			return fmt.Errorf("waiting for connection to %s: %w", conn.Target(), ctx.Err())
		}
	}
}

func (n *NodeClient) resetPubConn(conn *grpc.ClientConn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pubConn != nil && n.pubConn == conn {
		n.pubConn.Close()
		n.pubConn = nil
	}
}

func (n *NodeClient) ensurePrivConn(ctx context.Context) (*grpc.ClientConn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.privConn != nil {
		return n.privConn, nil
	}
	pod, err := waitPod(n.session, n.Name)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(
		fmt.Sprintf("%s:%d", pod.Status.PodIP, n.GRPC_PRIV),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	if err := n.waitForConnectionReady(ctx, conn); err != nil {
		return nil, err
	}
	n.privConn = conn
	return n.privConn, nil
}

func (n *NodeClient) resetPrivConn(conn *grpc.ClientConn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.privConn != nil && n.privConn == conn {
		n.privConn.Close()
		n.privConn = nil
	}
}

func (n *NodeClient) PubConn() *PubNodeClient {
	return &PubNodeClient{n}
}

type PubNodeClient struct {
	*NodeClient
}

func (n *PubNodeClient) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	conn, err := n.ensurePubConn(ctx)
	if err != nil {
		return err
	}
	err = conn.Invoke(ctx, method, args, reply, opts...)
	if err != nil {
		s, _ := status.FromError(err)
		if s.Code() != codes.InvalidArgument && s.Code() != codes.Canceled {
			// check for app error. this is not exhaustive.
			// the goal is to reset connection if pods were redeployed and changed IP
			n.resetPubConn(conn)
		}
	}
	return err
}

func (n *PubNodeClient) NewStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	conn, err := n.ensurePubConn(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := n.pubConn.NewStream(ctx, desc, method, opts...)
	if err != nil {
		n.resetPubConn(conn)
	}
	return stream, err
}

func (n *NodeClient) PrivConn() *PrivNodeClient {
	return &PrivNodeClient{n}
}

type PrivNodeClient struct {
	*NodeClient
}

func (n *PrivNodeClient) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	conn, err := n.ensurePrivConn(ctx)
	if err != nil {
		return err
	}
	err = conn.Invoke(ctx, method, args, reply, opts...)
	if err != nil {
		s, _ := status.FromError(err)
		if s.Code() != codes.InvalidArgument && s.Code() != codes.Canceled {
			// check for app error. this is not exhaustive.
			// the goal is to reset connection if pods were redeployed and changed IP
			n.resetPrivConn(conn)
		}
	}
	return err
}

func (n *PrivNodeClient) NewStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	conn, err := n.ensurePrivConn(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := n.privConn.NewStream(ctx, desc, method, opts...)
	if err != nil {
		n.resetPrivConn(conn)
	}
	return stream, err
}

func deployCertifierD(ctx *testcontext.Context, id, privkey string) (*NodeClient, error) {
	args := []string{
		"-c" + configDir + attachedCertifierConfig,
	}

	ctx.Log.Debugw("deploying certifier service pod", "id", id, "args", args, "image", ctx.CertifierImage)

	labels := nodeLabels(certifierApp, id)

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
						WithConfigMap(corev1.ConfigMapVolumeSource().WithName(certifierConfigMapName)),
					).
					WithContainers(corev1.Container().
						WithName("certifier").
						WithImage(ctx.CertifierImage).
						WithArgs(args...).
						WithEnv(corev1.EnvVar().WithName("CERTIFIER_SIGNING_KEY").WithValue(privkey)).
						WithPorts(
							corev1.ContainerPort().WithProtocol("TCP").WithContainerPort(certifierPort),
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

	_, err := ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return nil, fmt.Errorf("creating certifier: %w", err)
	}
	return &NodeClient{
		session: ctx,
		Node: Node{
			Name: id,
		},
	}, nil
}

func deployPoetD(ctx *testcontext.Context, id string, flags ...DeploymentFlag) (*NodeClient, error) {
	args := []string{
		"-c=" + configDir + attachedPoetConfig,
		"--metrics-port=" + strconv.Itoa(prometheusScrapePort),
	}
	for _, flag := range flags {
		args = append(args, flag.Flag())
	}

	pubkey, privkey := MakePoetKey(decodePoetIdentifier(id))
	keyb64 := base64.StdEncoding.EncodeToString(privkey)

	ctx.Log.Debugw("deploying poet pod", "id", id, "args", args, "image", ctx.PoetImage, "pubkey", pubkey)

	labels := nodeLabels(poetApp, id)
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
							corev1.ContainerPort().WithName("prometheus").WithContainerPort(prometheusScrapePort),
						).
						WithVolumeMounts(
							corev1.VolumeMount().WithName("config").WithMountPath(configDir),
						).
						WithResources(corev1.ResourceRequirements().
							WithRequests(poetResources.Get(ctx.Parameters).Requests).
							WithLimits(poetResources.Get(ctx.Parameters).Limits),
						).
						WithEnv(
							corev1.EnvVar().WithName("POET_PRIVATE_KEY").WithValue(keyb64),
						),
					),
				)))

	_, err := ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
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
				corev1.ServicePort().WithName("grpc-pub").WithPort(9092).WithProtocol("TCP"),
				corev1.ServicePort().WithName("grpc-priv").WithPort(9093).WithProtocol("TCP"),
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

func deployNodeSvc(ctx *testcontext.Context, id string) error {
	labels := nodeLabels(smesherApp, id)
	svc := corev1.Service(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("p2p").WithPort(7513).WithProtocol("TCP"),
				corev1.ServicePort().WithName("grpc-pub").WithPort(9092).WithProtocol("TCP"),
				corev1.ServicePort().WithName("grpc-priv").WithPort(9093).WithProtocol("TCP"),
				corev1.ServicePort().WithName("grpc-post").WithPort(9094).WithProtocol("TCP"),
			).
			WithClusterIP("None"),
		)
	_, err := ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("apply headless service: %w", err)
	}
	return nil
}

func deployCertifierSvc(ctx *testcontext.Context, id string) (*apiv1.Service, error) {
	ctx.Log.Debugw("deploying certifier service", "id", id)
	labels := nodeLabels(certifierApp, id)
	svc := corev1.Service(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("rest").WithPort(certifierPort).WithProtocol("TCP"),
			),
		)

	return ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
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
				corev1.ServicePort().WithName("prometheus").WithPort(prometheusScrapePort),
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

// deployCertifier creates a certifier Deployment and exposes it via a Service.
// The key is passed to the certifier Pod.
func deployCertifier(ctx *testcontext.Context, id, privkey string) (*NodeClient, error) {
	if _, err := deployCertifierSvc(ctx, id); err != nil {
		return nil, fmt.Errorf("deploying certifier service: %w", err)
	}

	node, err := deployCertifierD(ctx, id, privkey)
	if err != nil {
		return nil, err
	}

	return node, nil
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
	errPod := ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		DeleteCollection(ctx, apimetav1.DeleteOptions{}, apimetav1.ListOptions{LabelSelector: labelSelector(id)})
	var errSvc error
	if svcs, err := ctx.Client.CoreV1().Services(ctx.Namespace).
		List(ctx, apimetav1.ListOptions{LabelSelector: labelSelector(id)}); err == nil {
		for _, svc := range svcs.Items {
			err = ctx.Client.CoreV1().
				Services(ctx.Namespace).
				Delete(ctx, svc.ObjectMeta.Name, apimetav1.DeleteOptions{})
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, open := <-watcher.ResultChan():
			if !open {
				return nil, fmt.Errorf("watcher is terminated while waiting for pod with id %v", id)
			}
			pod, ok := ev.Object.(*apiv1.Pod)
			if !ok {
				continue
			}
			if pod.Status.Phase == apiv1.PodRunning && areContainersReady(pod) {
				return pod, nil
			}
		}
	}
}

func nodeLabels(name, id string) map[string]string {
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

func deployNodes(ctx *testcontext.Context, kind string, from, to int, opts ...DeploymentOpt) ([]*NodeClient, error) {
	ctx.Log.Debugw("deploying nodes", "kind", kind, "from", from, "to", to)
	var (
		eg      errgroup.Group
		clients = make(chan *NodeClient, to-from)
		cfg     = SmesherDeploymentConfig{}
	)
	for _, opt := range opts {
		opt(&cfg)
	}
	if delta := to - from; len(cfg.keys) > 0 && len(cfg.keys) != delta {
		return nil, fmt.Errorf(
			"keys must be overwritten for all or no members of the cluster: delta %d, keys %d %v",
			delta,
			len(cfg.keys),
			cfg.keys,
		)
	}
	for i := from; i < to; i++ {
		finalFlags := make([]DeploymentFlag, len(cfg.flags), len(cfg.flags)+ctx.PoetSize)
		copy(finalFlags, cfg.flags)
		if !cfg.noDefaultPoets {
			var poetIds []int
			for idx := 0; idx < ctx.PoetSize; idx++ {
				poetIds = append(poetIds, idx)
			}
			finalFlags = append(finalFlags, PoetEndpoints(poetIds...))
		}
		if ctx.BootstrapperSize > 1 {
			finalFlags = append(finalFlags, BootstrapperUrl(BootstrapperEndpoint(i%ctx.BootstrapperSize)))
		} else {
			finalFlags = append(finalFlags, BootstrapperUrl(BootstrapperEndpoint(0)))
		}
		var key ed25519.PrivateKey
		if len(cfg.keys) > 0 {
			key = cfg.keys[i-from]
		}

		eg.Go(func() error {
			id := fmt.Sprintf("%s-%d", kind, i)
			labels := nodeLabels(kind, id)
			labels["bucket"] = strconv.Itoa(i % buckets)
			if err := deployNode(ctx, id, key, labels, finalFlags); err != nil {
				return err
			}
			clients <- &NodeClient{
				session: ctx,
				Node: Node{
					Name:      id,
					P2P:       7513,
					GRPC_PUB:  9092,
					GRPC_PRIV: 9093,
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

func deployRemoteNodes(
	ctx *testcontext.Context,
	from, to int,
	goldenAtxId types.ATXID,
	opts ...DeploymentOpt,
) ([]*NodeClient, error) {
	ctx.Log.Debugw("deploying remote nodes", "from", from, "to", to)
	var (
		eg      errgroup.Group
		clients = make(chan *NodeClient, to-from)
		cfg     = SmesherDeploymentConfig{}
	)
	for _, opt := range opts {
		opt(&cfg)
	}
	if delta := to - from; len(cfg.keys) != delta {
		return nil, fmt.Errorf(
			"keys must be overwritten for all members of the cluster: delta %d, keys %d %v",
			delta,
			len(cfg.keys),
			cfg.keys,
		)
	}
	for i := from; i < to; i++ {
		finalFlags := make([]DeploymentFlag, len(cfg.flags), len(cfg.flags)+ctx.PoetSize)
		copy(finalFlags, cfg.flags)
		if !cfg.noDefaultPoets {
			var poetIds []int
			for idx := 0; idx < ctx.PoetSize; idx++ {
				poetIds = append(poetIds, idx)
			}
			finalFlags = append(finalFlags, PoetEndpoints(poetIds...))
		}
		if ctx.BootstrapperSize > 1 {
			finalFlags = append(finalFlags, BootstrapperUrl(BootstrapperEndpoint(i%ctx.BootstrapperSize)))
		} else {
			finalFlags = append(finalFlags, BootstrapperUrl(BootstrapperEndpoint(0)))
		}

		nodeId := fmt.Sprintf("%s-%d", smesherApp, i)
		eg.Go(func() error {
			labels := nodeLabels(smesherApp, nodeId)
			labels["bucket"] = strconv.Itoa(i % buckets)
			if err := deployNode(ctx, nodeId, cfg.keys[i-from], labels, finalFlags); err != nil {
				return err
			}
			deployNodeSvc(ctx, nodeId)
			clients <- &NodeClient{
				session: ctx,
				Node: Node{
					Name:      nodeId,
					P2P:       7513,
					GRPC_PUB:  9092,
					GRPC_PRIV: 9093,
				},
			}
			return nil
		})

		eg.Go(func() error {
			postId := fmt.Sprintf("%s-%d", postServiceApp, i)
			labels := nodeLabels(postServiceApp, postId)
			labels["bucket"] = strconv.Itoa(i % buckets)
			labels["nodeId"] = nodeId
			err := deployPostService(ctx, postId, labels, nodeId,
				hex.EncodeToString(cfg.keys[i-from].Public().(ed25519.PublicKey)),
				goldenAtxId.Hash32().String(),
			)
			return err
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
	// find and delete any post services linked to this node
	var errService error
	if svcs, err := ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		List(ctx, apimetav1.ListOptions{LabelSelector: fmt.Sprintf("nodeId=%s", id)}); err == nil {
		for _, svc := range svcs.Items {
			err = ctx.Client.AppsV1().Deployments(ctx.Namespace).
				Delete(ctx, svc.ObjectMeta.Name, apimetav1.DeleteOptions{})
			errService = errors.Join(errService, err)
		}
	}
	if errService != nil {
		return errService
	}

	if err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Delete(ctx, id, apimetav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting configmap %s/%s: %w", ctx.Namespace, id, err)
	}
	if err := ctx.Client.AppsV1().Deployments(ctx.Namespace).
		Delete(ctx, id, apimetav1.DeleteOptions{}); err != nil {
		return err
	}
	return nil
}

func deployNode(
	ctx *testcontext.Context,
	id string,
	key ed25519.PrivateKey,
	labels map[string]string,
	flags []DeploymentFlag,
) error {
	ctx.Log.Debugw("deploying node", "id", id)
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

	podSpec := corev1.PodSpec().
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
				corev1.ContainerPort().WithContainerPort(9092).WithName("grpc-pub"),
				corev1.ContainerPort().WithContainerPort(9093).WithName("grpc-priv"),
				corev1.ContainerPort().WithContainerPort(9094).WithName("grpc-post"),
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
					corev1.TCPSocketAction().WithPort(intstr.FromInt32(9092)),
				).WithInitialDelaySeconds(10).WithPeriodSeconds(10),
			).
			WithEnv(
				corev1.EnvVar().WithName("GOMAXPROCS").WithValue("4"),
			).
			WithCommand(cmd...),
		)

	if key != nil {
		podSpec = podSpec.
			WithInitContainers(
				corev1.Container().
					WithName("file-creator").
					WithImage("busybox").
					WithCommand("sh", "-c",
						fmt.Sprintf("mkdir -p /data/identities && echo '%x' > /data/identities/local.key", key),
					).
					WithVolumeMounts(
						corev1.VolumeMount().WithName("data").WithMountPath("/data"),
					),
			)
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
				WithSpec(podSpec),
			),
		)
	_, err := ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("apply pod %s: %w", id, err)
	}
	if strings.Contains(id, bootnodeApp) {
		return deployBootnodeSvc(ctx, id)
	}
	return nil
}

func loadSmesherConfig(ctx *testcontext.Context) (*config.Config, error) {
	// TODO(poszu): this is mostly a copy of the code in cmd/node.go
	// refactor the code below to reuse it after https://github.com/spacemeshos/go-spacemesh/pull/5485 lands.
	vip := viper.New()
	vip.SetConfigType("json")
	if err := vip.ReadConfig(strings.NewReader(smesherConfig.Get(ctx.Parameters))); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	conf := config.MainnetConfig()
	if name := vip.GetString("preset"); len(name) > 0 {
		preset, err := presets.Get(name)
		if err != nil {
			return nil, err
		}
		conf = preset
	}

	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.AddressListDecodeFunc(),
		mapstructureutil.BigRatDecodeFunc(),
		mapstructureutil.PostProviderIDDecodeFunc(),
		mapstructureutil.DeprecatedHook(),
		mapstructure.TextUnmarshallerHookFunc(),
	)
	opts := []viper.DecoderConfigOption{
		viper.DecodeHook(hook),
		node.WithZeroFields(),
		node.WithIgnoreUntagged(),
	}
	if err := vip.Unmarshal(&conf, opts...); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	return &conf, nil
}

func deployPostService(
	ctx *testcontext.Context,
	id string,
	labels map[string]string,
	nodeId string,
	pubKey string,
	goldenAtxId string,
) error {
	ctx.Log.Debugw("deploying post service", "id", id)
	conf, err := loadSmesherConfig(ctx)
	if err != nil {
		return fmt.Errorf("loading smesher config: %w", err)
	}
	args := []string{
		"--dir", "/data",
		"--address", fmt.Sprintf("http://%s:%d", nodeId, 9094),
		"--threads", strconv.FormatUint(uint64(conf.SMESHING.ProvingOpts.Threads), 10),
		"--nonces", strconv.FormatUint(uint64(conf.SMESHING.ProvingOpts.Nonces), 10),
		"--randomx-mode", conf.SMESHING.ProvingOpts.RandomXMode.String(),
		"--min-num-units", strconv.FormatUint(uint64(conf.POST.MinNumUnits), 10),
		"--max-num-units", strconv.FormatUint(uint64(conf.POST.MaxNumUnits), 10),
		"--k1", strconv.FormatUint(uint64(conf.POST.K1), 10),
		"--k2", strconv.FormatUint(uint64(conf.POST.K2), 10),
		"--pow-difficulty", conf.POST.PowDifficulty.String(),
		"-n", strconv.FormatUint(uint64(conf.SMESHING.Opts.Scrypt.N), 10),
		"-r", strconv.FormatUint(uint64(conf.SMESHING.Opts.Scrypt.R), 10),
		"-p", strconv.FormatUint(uint64(conf.SMESHING.Opts.Scrypt.P), 10),
	}
	initArgs := []string{
		"-id", pubKey,
		"-commitmentAtxId", goldenAtxId,
		"-datadir", "/data",
		"-numUnits", strconv.FormatUint(uint64(conf.POST.MinNumUnits), 10),
		"-labelsPerUnit", strconv.FormatUint(uint64(conf.POST.LabelsPerUnit), 10),
		"-scryptN", strconv.FormatUint(uint64(conf.SMESHING.Opts.Scrypt.N), 10),
		"-provider", "4294967295", // 0xffffffff = CPU Provider
		"-yes", // to prevent checks for mainnet compatibility
	}
	deployment := appsv1.Deployment(id, ctx.Namespace).
		WithLabels(labels).
		WithSpec(appsv1.DeploymentSpec().
			WithSelector(metav1.LabelSelector().WithMatchLabels(labels)).
			WithReplicas(1).
			WithTemplate(corev1.PodTemplateSpec().
				WithLabels(labels).
				WithSpec(corev1.PodSpec().
					WithInitContainers(corev1.Container().
						WithName("init").
						WithImage(ctx.PostInitImage).
						WithImagePullPolicy(apiv1.PullIfNotPresent).
						WithArgs(initArgs...).
						WithVolumeMounts(
							corev1.VolumeMount().WithName("data").WithMountPath("/data"),
						),
					).
					WithNodeSelector(ctx.NodeSelector).
					WithVolumes(
						corev1.Volume().WithName("data").
							WithEmptyDir(corev1.EmptyDirVolumeSource().
								WithSizeLimit(resource.MustParse(ctx.Storage.Size))),
					).
					WithContainers(corev1.Container().
						WithName("post-service").
						WithImage(ctx.PostServiceImage).
						WithImagePullPolicy(apiv1.PullIfNotPresent).
						WithArgs(args...).
						WithVolumeMounts(
							corev1.VolumeMount().WithName("data").WithMountPath("/data"),
						).
						WithResources(corev1.ResourceRequirements().
							WithRequests(smesherResources.Get(ctx.Parameters).Requests).
							WithLimits(smesherResources.Get(ctx.Parameters).Limits),
						),
					),
				),
			),
		)
	_, err = ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("create post service %s: %w", id, err)
	}
	return nil
}

func deployBootstrapper(
	ctx *testcontext.Context,
	id string,
	bsEpochs []int,
	flags ...DeploymentFlag,
) (*NodeClient, error) {
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

func deployBootstrapperD(
	ctx *testcontext.Context,
	id string,
	bsEpochs []int,
	flags ...DeploymentFlag,
) (*NodeClient, error) {
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
							corev1.ContainerPort().
								WithName("rest").
								WithProtocol("TCP").
								WithContainerPort(bootstrapperPort),
						).
						WithResources(corev1.ResourceRequirements().
							WithRequests(bootstrapperResources.Get(ctx.Parameters).Requests).
							WithLimits(bootstrapperResources.Get(ctx.Parameters).Limits),
						).
						WithCommand(cmd...),
					),
				)))

	_, err := ctx.Client.AppsV1().
		Deployments(ctx.Namespace).
		Apply(ctx, deployment, apimetav1.ApplyOptions{FieldManager: "test"})
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

func PoetEndpoints(ids ...int) DeploymentFlag {
	var poets []types.PoetServer
	for _, id := range ids {
		pubkey, _ := MakePoetKey(id)
		poets = append(poets, types.PoetServer{
			Address: MakePoetEndpoint(id),
			Pubkey:  types.NewBase64Enc(pubkey),
		})
	}
	value, err := json.Marshal(poets)
	if err != nil {
		panic(err)
	}
	return DeploymentFlag{Name: "--poet-servers", Value: string(value)}
}

func PostK3(k3 int) DeploymentFlag {
	return DeploymentFlag{Name: "--post-k3", Value: strconv.Itoa(k3)}
}

// MinPeers flag.
func MinPeers(target int) DeploymentFlag {
	return DeploymentFlag{Name: "--min-peers", Value: strconv.Itoa(target)}
}

func BootstrapperUrl(endpoint string) DeploymentFlag {
	return DeploymentFlag{Name: "--bootstrap-url", Value: endpoint}
}

func CheckpointUrl(endpoint string) DeploymentFlag {
	return DeploymentFlag{Name: "--recovery-uri", Value: endpoint}
}

func CheckpointLayer(restoreLayer uint32) DeploymentFlag {
	return DeploymentFlag{Name: "--recovery-layer", Value: strconv.Itoa(int(restoreLayer))}
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

func PoetCertifierURL(url string) DeploymentFlag {
	return DeploymentFlag{Name: "--certifier-url", Value: url}
}

func PoetCertifierPubkey(key string) DeploymentFlag {
	return DeploymentFlag{Name: "--certifier-pubkey", Value: key}
}

// PoetRestListen socket pair with http api.
func PoetRestListen(port int) DeploymentFlag {
	return DeploymentFlag{Name: "--restlisten", Value: fmt.Sprintf("0.0.0.0:%d", port)}
}

func StartSmeshing(start bool) DeploymentFlag {
	return DeploymentFlag{Name: "--smeshing-start", Value: strconv.FormatBool(start)}
}

func GenerateFallback() DeploymentFlag {
	return DeploymentFlag{Name: "--fallback", Value: strconv.FormatBool(true)}
}

func Bootnode() DeploymentFlag {
	return DeploymentFlag{Name: "--p2p-bootnode", Value: strconv.FormatBool(true)}
}

func PrivateNetwork() DeploymentFlag {
	return DeploymentFlag{Name: "--p2p-private-network", Value: strconv.FormatBool(true)}
}
