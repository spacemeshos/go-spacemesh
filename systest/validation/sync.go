package validation

import (
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
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

func RunSyncValidation(ctx context.Context, c *cluster.Cluster, period time.Duration, tolerate int) error {
	failures := make([]int, c.Total())
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
						failures[i]++
					} else {
						failures[i] = 0
					}
					return nil
				})
			}
			eg.Wait()
			for i, rst := range failures {
				if rst > tolerate {
					return fmt.Errorf("node %s wasn't able to sync in %d periods",
						c.Client(i).Name, rst,
					)
				}
			}
		}
	}
}
