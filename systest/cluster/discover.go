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

func discoverNodes(ctx *testcontext.Context, kind string) ([]*NodeClient, error) {
	deployments, err := ctx.Client.AppsV1().Deployments(ctx.Namespace).List(ctx,
		apimetav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", kind)})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods app=%s: %w", kind, err)
	}
	var (
		eg      errgroup.Group
		clients = make(chan *NodeClient, len(deployments.Items))
		rst     []*NodeClient
	)
	for _, deployment := range deployments.Items {
		deployment := deployment
		eg.Go(func() error {
			client, err := waitNode(ctx, deployment.Name)
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
	// expected name is boot-1-0 or poet-3213221321
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		panic(fmt.Sprintf("unexpected name format %s", name))
	}
	ord, err := strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}
	return ord
}
