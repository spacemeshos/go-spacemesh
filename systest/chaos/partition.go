package chaos

import (
	"context"
	"fmt"

	chaosv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// Partition2 partitions pods in array a from pods in array b.
func Partition2(ctx *testcontext.Context, name string, a, b []string) (Teardown, error) {
	partition := chaosv1alpha1.NetworkChaos{}
	partition.Name = name
	partition.Namespace = ctx.Namespace

	partition.Spec.Action = chaosv1alpha1.PartitionAction
	partition.Spec.Mode = chaosv1alpha1.AllMode
	partition.Spec.Selector.Pods = map[string][]string{
		ctx.Namespace: a,
	}
	partition.Spec.Direction = chaosv1alpha1.Both
	partition.Spec.Target = &chaosv1alpha1.PodSelector{
		Mode: chaosv1alpha1.AllMode,
	}
	partition.Spec.Target.Selector.Pods = map[string][]string{
		ctx.Namespace: b,
	}

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
