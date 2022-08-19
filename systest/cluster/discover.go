package cluster

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func discoverNodes(ctx *testcontext.Context, name string, pt PodType) ([]*NodeClient, error) {
	pods, err := ctx.Client.CoreV1().Pods(ctx.Namespace).List(ctx,
		apimetav1.ListOptions{LabelSelector: labelSelector(nodeLabels(name))})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods name=%s: %w", name, err)
	}
	var (
		eg      errgroup.Group
		clients = make(chan *NodeClient, len(pods.Items))
		rst     []*NodeClient
	)
	for _, pod := range pods.Items {
		pod := pod
		eg.Go(func() error {
			client, err := waitNode(ctx, pod.Name, pt)
			if err != nil {
				return err
			}
			if client != nil {
				clients <- client
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(clients)
	for node := range clients {
		rst = append(rst, node)
	}
	sort.Slice(rst, func(i, j int) bool {
		return decodeOrdinal(rst[i].Name) < decodeOrdinal(rst[j].Name)
	})
	return rst, nil
}

func decodeOrdinal(name string) int {
	// expected name is boot-1-0
	parts := strings.Split(name, "-")
	if len(parts) != 3 {
		panic(fmt.Sprintf("unexpected name format %s", name))
	}
	ord, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}
	return ord
}
