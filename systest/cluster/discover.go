package cluster

import (
	"fmt"

	"golang.org/x/sync/errgroup"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func discoverNodes(ctx *testcontext.Context, name string) ([]*NodeClient, error) {
	pods, err := ctx.Client.CoreV1().Pods(ctx.Namespace).List(ctx,
		v1.ListOptions{LabelSelector: labelSelector(nodeLabels(name))})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods name=%s: %w", name, err)
	}
	var (
		eg      errgroup.Group
		clients = make([]*NodeClient, len(pods.Items))
	)
	for i, pod := range pods.Items {
		i := i
		pod := pod
		eg.Go(func() error {
			client, err := waitSmesher(ctx, pod.Name)
			if err != nil {
				return err
			}
			clients[i] = client
			ctx.Log.Debugw("discovered existing smesher", "name", pod.Name)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return clients, nil
}
