package validation

import (
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func isSynced(ctx context.Context, node *cluster.NodeClient) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc := pb.NewNodeServiceClient(node)
	resp, err := svc.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return false
	}
	return resp.Status.IsSynced
}

func RunSyncValidation(ctx context.Context, tctx *testcontext.Context, c *cluster.Cluster, period time.Duration, tolerate int) error {
	results := make([]int, c.Total())
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var eg errgroup.Group
			for i := 0; i < c.Total(); i++ {
				i := i
				node := c.Client(i)
				eg.Go(func() error {
					if !isSynced(ctx, node) {
						results[i]++
					} else {
						results[i] = 0
					}
					return nil
				})
			}
			eg.Wait()
			for i, rst := range results {
				if rst != 0 {
					tctx.Log.Debugw("node is not synced", "node", c.Client(i).Name)
				}
				if rst > tolerate {
					return fmt.Errorf("node %s wasn't able to sync in %d periods",
						c.Client(i).Name, rst,
					)
				}
			}
		}
	}
}
