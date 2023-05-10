package chaos

import (
	"context"
	"fmt"

	chaos "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// Partition2 partitions pods in array a from pods in array b.
func Partition2(ctx *testcontext.Context, name string, a, b []string) (Teardown, error) {
	partition := chaos.NetworkChaos{}
	partition.Name = name
	partition.Namespace = ctx.Namespace

	partition.Spec.Action = chaos.PartitionAction
	partition.Spec.Mode = chaos.AllMode
	partition.Spec.Selector = selectPods(a)

	partition.Spec.Direction = chaos.Both
	partition.Spec.Target = &chaos.PodSelector{
		Mode: chaos.AllMode,
	}
	partition.Spec.Target.Selector = selectPods(b)

	desired := partition.DeepCopy()
	_, err := controllerutil.CreateOrUpdate(ctx, ctx.Generic, &partition, func() error {
		partition.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating partition for %v | %v: %w", a, b, err)
	}

	return func(rctx context.Context) error {
		return ctx.Generic.Delete(rctx, &partition)
	}, nil
}
