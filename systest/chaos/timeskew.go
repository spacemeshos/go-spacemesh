package chaos

import (
	"context"

	chaos "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// Timeskew adjusts CLOCK_REALTIME on the specified pods by the offset.
func Timeskew(ctx context.Context, tctx *testcontext.Context, name string, offset string, pods ...string) (Teardown, error) {
	tc := chaos.TimeChaos{}
	tc.Name = name
	tc.Namespace = tctx.Namespace

	tc.Spec.Mode = chaos.AllMode
	tc.Spec.Selector = chaos.PodSelectorSpec{
		Pods: map[string][]string{
			tctx.Namespace: pods,
		},
	}
	tc.Spec.TimeOffset = offset

	if err := tctx.Generic.Create(ctx, &tc); err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		return tctx.Generic.Delete(ctx, &tc)
	}, nil
}
