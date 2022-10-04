package cluster

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	apiappsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	appsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

const persistentVolumeName = "data"

func persistentVolumeClaim(podname string) string {
	return fmt.Sprintf("%s-%s", persistentVolumeName, podname)
}

const prometheusScrapePort = 9216

// Node ...
type Node struct {
	Name      string
	IP        string
	P2P, GRPC uint16
	ID        string

	Created   time.Time
	Restarted time.Time
}

// GRPCEndpoint returns grpc endpoint for the Node.
func (n Node) GRPCEndpoint() string {
	return fmt.Sprintf("%s:%d", n.IP, n.GRPC)
}

// P2PEndpoint returns full p2p endpoint, including identity.
func (n Node) P2PEndpoint() string {
	return fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", n.IP, n.P2P, n.ID)
}

// NodeClient is a Node with attached grpc connection.
type NodeClient struct {
	Node
	*grpc.ClientConn
}

// deployPoet accepts address of the gateway (to use dns resolver add dns:/// prefix to the address)
// and output ip of the poet.
func deployPoet(ctx *testcontext.Context, name string, gateways []string, flags ...DeploymentFlag) (*NodeClient, error) {
	var args []string
	for _, flag := range flags {
		args = append(args, flag.Flag())
	}
	for _, gw := range gateways {
		args = append(args, "--gateway="+gw)
	}
	ctx.Log.Debugw("deploying poet pod", "args", args, "image", ctx.PoetImage)
	labels := nodeLabels("poet")
	pod := corev1.Pod(name, ctx.Namespace).
		WithLabels(labels).
		WithSpec(
			corev1.PodSpec().
				WithNodeSelector(ctx.NodeSelector).
				WithContainers(corev1.Container().
					WithName("poet").
					WithImage(ctx.PoetImage).
					WithArgs(args...).
					WithPorts(corev1.ContainerPort().WithName("rest").WithProtocol("TCP").WithContainerPort(poetPort)).
					WithResources(corev1.ResourceRequirements().WithRequests(
						v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0.5"),
							v1.ResourceMemory: resource.MustParse("1Gi"),
						},
					)),
				),
		)
	_, err := ctx.Client.CoreV1().Pods(ctx.Namespace).Apply(ctx, pod, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return nil, fmt.Errorf("create poet: %w", err)
	}
	svc := corev1.Service(name, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("rest").WithPort(poetPort).WithProtocol("TCP"),
			),
		)
	_, err = ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return nil, fmt.Errorf("apply poet service: %w", err)
	}

	ppod, err := waitNode(ctx, *pod.Name, Poet)
	if err != nil {
		return nil, err
	}
	return ppod, nil
}

func deletePoet(ctx *testcontext.Context, name string) error {
	return ctx.Client.CoreV1().Pods(ctx.Namespace).Delete(ctx, name, apimetav1.DeleteOptions{})
}

func getStatefulSet(ctx *testcontext.Context, name string) (*apiappsv1.StatefulSet, error) {
	set, err := ctx.Client.AppsV1().StatefulSets(ctx.Namespace).Get(ctx, name, apimetav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return set, nil
}

func waitPod(ctx *testcontext.Context, name string) (*v1.Pod, error) {
	watcher, err := ctx.Client.CoreV1().Pods(ctx.Namespace).Watch(ctx, apimetav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", name),
	})
	if err != nil {
		return nil, err
	}
	defer watcher.Stop()
	for {
		var pod *v1.Pod
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, open := <-watcher.ResultChan():
			if !open {
				return nil, fmt.Errorf("watcher is terminated while waiting for pod %v", name)
			}
			if ev.Type == watch.Deleted {
				return nil, nil
			}
			pod = ev.Object.(*v1.Pod)
		}
		switch pod.Status.Phase {
		case v1.PodFailed:
			return nil, fmt.Errorf("pod failed %s", name)
		case v1.PodRunning:
			return pod, nil
		}
	}
}

func nodeLabels(name string) map[string]string {
	return map[string]string{
		"app": name,
	}
}

func labelSelector(labels map[string]string) string {
	var (
		buf   strings.Builder
		first = true
	)
	for key, value := range labels {
		if !first {
			buf.WriteString(",")
		}
		first = false
		buf.WriteString(key)
		buf.WriteString("=")
		buf.WriteString(value)
	}
	return buf.String()
}

func deployNodes(ctx *testcontext.Context, name string, from, to int, flags []DeploymentFlag) ([]*NodeClient, error) {
	var (
		labels  = nodeLabels(name)
		eg      errgroup.Group
		clients = make(chan *NodeClient, to-from)
	)
	for i := from; i < to; i++ {
		i := i
		finalFlags := make([]DeploymentFlag, len(flags)+1)
		copy(finalFlags, flags)
		eg.Go(func() error {
			setname := fmt.Sprintf("%s-%d", name, i)
			idx := i % ctx.PoetSize
			finalFlags[len(finalFlags)-1] = PoetEndpoint(MakePoetEndpoint(idx))
			if err := deployNode(ctx, setname, labels, finalFlags); err != nil {
				return err
			}
			podname := fmt.Sprintf("%s-0", setname)
			node, err := waitNode(ctx, podname, Smesher)
			if err != nil {
				return err
			}
			clients <- node
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

func deleteNode(ctx *testcontext.Context, podname string) error {
	setname := setName(podname)
	if err := ctx.Client.AppsV1().StatefulSets(ctx.Namespace).
		Delete(ctx, setname, apimetav1.DeleteOptions{}); err != nil {
		return err
	}
	pvcname := persistentVolumeClaim(podname)
	if err := ctx.Client.CoreV1().PersistentVolumeClaims(ctx.Namespace).Delete(ctx,
		pvcname, apimetav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed deleting pvc %s: %w", pvcname, err)
	}
	return nil
}

func deployNode(ctx *testcontext.Context, name string, applabels map[string]string, flags []DeploymentFlag) error {
	labels := map[string]string{
		"smesher": name,
	}
	for label, value := range applabels {
		labels[label] = value
	}
	svc := corev1.Service(headlessSvc(name), ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("grpc").WithPort(9092).WithProtocol("TCP"),
			).
			WithClusterIP("None"),
		)

	_, err := ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("apply headless service: %w", err)
	}
	cmd := []string{
		"/bin/go-spacemesh",
		"--pprof-server",
		"--preset=fastnet",
		"--smeshing-start=true",
		"--smeshing-opts-datadir=/data/post",
		"-d=/data/state",
		"--log-encoder=json",
		"--metrics",
		"--metrics-port=" + strconv.Itoa(prometheusScrapePort),
	}
	for _, flag := range flags {
		cmd = append(cmd, flag.Flag())
	}

	sset := appsv1.StatefulSet(name, ctx.Namespace).
		WithSpec(appsv1.StatefulSetSpec().
			WithUpdateStrategy(appsv1.StatefulSetUpdateStrategy().WithType(apiappsv1.OnDeleteStatefulSetStrategyType)).
			WithPodManagementPolicy(apiappsv1.ParallelPodManagement).
			WithReplicas(1).
			WithServiceName(*svc.Name).
			WithVolumeClaimTemplates(
				corev1.PersistentVolumeClaim(persistentVolumeName, ctx.Namespace).
					WithSpec(corev1.PersistentVolumeClaimSpec().
						WithAccessModes(v1.ReadWriteOnce).
						WithStorageClassName(ctx.Storage.Class).
						WithResources(corev1.ResourceRequirements().
							WithRequests(v1.ResourceList{v1.ResourceStorage: resource.MustParse(ctx.Storage.Size)}))),
			).
			WithSelector(metav1.LabelSelector().WithMatchLabels(labels)).
			WithTemplate(corev1.PodTemplateSpec().
				WithAnnotations(
					map[string]string{
						"prometheus.io/port":   strconv.Itoa(prometheusScrapePort),
						"prometheus.io/scrape": "true",
					},
				).
				WithLabels(labels).
				WithSpec(corev1.PodSpec().
					WithNodeSelector(ctx.NodeSelector).
					WithContainers(corev1.Container().
						WithName("smesher").
						WithImage(ctx.Image).
						WithImagePullPolicy(v1.PullIfNotPresent).
						WithPorts(
							corev1.ContainerPort().WithContainerPort(7513).WithName("p2p"),
							corev1.ContainerPort().WithContainerPort(9092).WithName("grpc"),
							corev1.ContainerPort().WithContainerPort(prometheusScrapePort).WithName("prometheus"),
						).
						WithVolumeMounts(
							corev1.VolumeMount().WithName("data").WithMountPath("/data"),
						).
						WithResources(corev1.ResourceRequirements().WithRequests(
							v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("0.5"),
								v1.ResourceMemory: resource.MustParse("200Mi"),
							},
						)).
						WithEnv(
							corev1.EnvVar().WithName("GOMAXPROCS").WithValue("2"),
						).
						WithCommand(cmd...),
					)),
			),
		)

	_, err = ctx.Client.AppsV1().StatefulSets(ctx.Namespace).
		Apply(ctx, sset, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return fmt.Errorf("apply statefulset: %w", err)
	}
	return nil
}

// PodType indicates the type of pod.
type PodType int

const (
	// Smesher ...
	Smesher PodType = iota
	// Poet ...
	Poet
)

func waitNode(tctx *testcontext.Context, podname string, pt PodType) (*NodeClient, error) {
	attempt := func() (*NodeClient, error) {
		pod, err := waitPod(tctx, podname)
		if err != nil {
			return nil, err
		}
		if pod == nil {
			return nil, nil
		}
		if pt == Poet {
			return &NodeClient{
				Node: Node{
					Name: podname,
				},
			}, nil
		}
		set, err := getStatefulSet(tctx, setName(podname))
		if err != nil {
			return nil, err
		}
		node := Node{
			Name:      podname,
			IP:        pod.Status.PodIP,
			P2P:       7513,
			GRPC:      9092,
			Created:   set.CreationTimestamp.Time,
			Restarted: pod.CreationTimestamp.Time,
		}
		rctx, cancel := context.WithTimeout(tctx, 2*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(rctx, node.GRPCEndpoint(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		if err != nil {
			return nil, err
		}
		dbg := spacemeshv1.NewDebugServiceClient(conn)
		info, err := dbg.NetworkInfo(rctx, &emptypb.Empty{})
		if err != nil {
			return nil, err
		}
		node.ID = info.Id
		return &NodeClient{
			Node:       node,
			ClientConn: conn,
		}, nil
	}
	const attempts = 10
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for i := 1; i <= attempts; i++ {
		if nc, err := attempt(); err != nil && i == attempts {
			return nil, err
		} else if err == nil {
			return nc, nil
		}
		select {
		case <-tctx.Done():
			return nil, tctx.Err()
		case <-ticker.C:
		}
	}
	panic("unreachable")
}

// DeploymentFlag allows to configure specific flags for application binaries.
type DeploymentFlag struct {
	Name, Value string
}

// Flag returns parseable flag from Name and Value.
func (d DeploymentFlag) Flag() string {
	return d.Name + "=" + d.Value
}

// RerunInterval flag.
func RerunInterval(duration time.Duration) DeploymentFlag {
	return DeploymentFlag{Name: "--tortoise-rerun-interval", Value: duration.String()}
}

// PoetEndpoint flag.
func PoetEndpoint(endpoint string) DeploymentFlag {
	return DeploymentFlag{Name: "--poet-server", Value: endpoint}
}

// TargetOutbound flag.
func TargetOutbound(target int) DeploymentFlag {
	return DeploymentFlag{Name: "--target-outbound", Value: strconv.Itoa(target)}
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

// EpochDuration ...
func EpochDuration(d time.Duration) DeploymentFlag {
	return DurationFlag("--epoch-duration", d)
}

// CycleGap ...
func CycleGap(d time.Duration) DeploymentFlag {
	return DurationFlag("--cycle-gap", d)
}

// PhaseShift ...
func PhaseShift(d time.Duration) DeploymentFlag {
	return DurationFlag("--phase-shift", d)
}

// GracePeriod ...
func GracePeriod(d time.Duration) DeploymentFlag {
	return DurationFlag("--grace-period", d)
}
