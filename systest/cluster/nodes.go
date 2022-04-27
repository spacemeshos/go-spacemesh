package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	apiappsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1 "k8s.io/client-go/applyconfigurations/core/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// Node ...
type Node struct {
	Name      string
	IP        string
	P2P, GRPC uint16
	ID        string
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
func deployPoet(ctx *testcontext.Context, gateways ...string) (string, error) {
	args := []string{}
	for _, gateway := range gateways {
		args = append(args, "--gateway="+gateway)
	}
	args = append(args,
		"--restlisten=0.0.0.0:"+strconv.Itoa(poetPort),
		"--duration=30s",
		"--n=10",
	)
	labels := nodeLabels("poet")
	pod := corev1.Pod("poet", ctx.Namespace).
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
		return "", fmt.Errorf("create poet: %w", err)
	}
	svc := corev1.Service(poetSvc, ctx.Namespace).
		WithLabels(labels).
		WithSpec(corev1.ServiceSpec().
			WithSelector(labels).
			WithPorts(
				corev1.ServicePort().WithName("rest").WithPort(poetPort).WithProtocol("TCP"),
			),
		)
	_, err = ctx.Client.CoreV1().Services(ctx.Namespace).Apply(ctx, svc, apimetav1.ApplyOptions{FieldManager: "test"})
	if err != nil {
		return "", fmt.Errorf("apply poet service: %w", err)
	}

	_, err = waitPod(ctx, *pod.Name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", *svc.Name, poetPort), nil
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
		case ev := <-watcher.ResultChan():
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
	var buf strings.Builder
	for key, value := range labels {
		buf.WriteString(key)
		buf.WriteString("=")
		buf.WriteString(value)
		buf.WriteString(",")
	}
	return buf.String()
}

func deployNodes(ctx *testcontext.Context, name string, replicas int, flags []DeploymentFlag) ([]*NodeClient, error) {
	labels := nodeLabels(name)
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
		return nil, fmt.Errorf("apply headless service: %w", err)
	}
	cmd := []string{
		"/bin/go-spacemesh",
		"--preset=fastnet",
		"--smeshing-start=true",
		"--smeshing-opts-datadir=/data/post",
		"-d=/data/state",
		"--log-encoder=json",
	}
	for _, flag := range flags {
		cmd = append(cmd, flag.Flag())
	}

	sset := appsv1.StatefulSet(name, ctx.Namespace).
		WithSpec(appsv1.StatefulSetSpec().
			WithUpdateStrategy(appsv1.StatefulSetUpdateStrategy().WithType(apiappsv1.OnDeleteStatefulSetStrategyType)).
			WithPodManagementPolicy(apiappsv1.ParallelPodManagement).
			WithReplicas(int32(replicas)).
			WithServiceName(*svc.Name).
			WithVolumeClaimTemplates(
				corev1.PersistentVolumeClaim("data", ctx.Namespace).
					WithSpec(corev1.PersistentVolumeClaimSpec().
						WithAccessModes(v1.ReadWriteOnce).
						WithStorageClassName("standard").
						WithResources(corev1.ResourceRequirements().
							WithRequests(v1.ResourceList{v1.ResourceStorage: resource.MustParse("1Gi")}))),
			).
			WithSelector(metav1.LabelSelector().WithMatchLabels(labels)).
			WithTemplate(corev1.PodTemplateSpec().
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
		return nil, fmt.Errorf("apply statefulset: %w", err)
	}
	result := make([]*NodeClient, replicas)
	var eg errgroup.Group
	for i := 0; i < replicas; i++ {
		i := i
		eg.Go(func() error {
			nc, err := waitSmesher(ctx, fmt.Sprintf("%s-%d", *sset.Name, i))
			if err != nil {
				return err
			}
			result[i] = nc
			return nil
		})
	}
	if eg.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

func waitSmesher(tctx *testcontext.Context, name string) (*NodeClient, error) {
	attempt := func() (*NodeClient, error) {
		pod, err := waitPod(tctx, name)
		if err != nil {
			return nil, err
		}
		node := Node{
			Name: name,
			IP:   pod.Status.PodIP,
			P2P:  7513,
			GRPC: 9092,
		}
		rctx, cancel := context.WithTimeout(tctx, 5*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(rctx, node.GRPCEndpoint(),
			grpc.WithInsecure(),
			grpc.WithBlock(),
		)
		if err != nil {
			return nil, err
		}
		dbg := spacemeshv1.NewDebugServiceClient(conn)
		info, err := dbg.NetworkInfo(tctx, &emptypb.Empty{})
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

// NetworkID flag.
func NetworkID(id uint32) DeploymentFlag {
	return DeploymentFlag{Name: "--network-id", Value: strconv.Itoa(int(id))}
}

// TargetOutbound flag.
func TargetOutbound(target int) DeploymentFlag {
	return DeploymentFlag{Name: "--target-outbound", Value: strconv.Itoa(target)}
}

// GenesisTime flag.
func GenesisTime(t time.Time) DeploymentFlag {
	return DeploymentFlag{Name: "--genesis-time", Value: t.Format(time.RFC3339)}
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
