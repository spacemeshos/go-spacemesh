package cluster

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func discoverNodes(ctx *testcontext.Context, kind string) ([]*NodeClient, error) {
	deployments, err := ctx.Client.AppsV1().Deployments(ctx.Namespace).List(ctx,
		apimetav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", kind)})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods app=%s: %w", kind, err)
	}
	var rst []*NodeClient
	for _, deployment := range deployments.Items {
		rst = append(rst, &NodeClient{
			session: ctx,
			Node: Node{
				Name:      deployment.Name,
				P2P:       7513,
				GRPC_PUB:  9092,
				GRPC_PRIV: 9093,
			},
		})
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
