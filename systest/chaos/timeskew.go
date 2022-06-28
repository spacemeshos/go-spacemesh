package chaos

import (
	"context"

	chaosv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"

	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// Timeskew adjusts CLOCK_REALTIME on the specified pods by the offset.
func Timeskew(cctx *testcontext.Context, name string, offset string, pods ...string) (Teardown, error) {
	tc := chaosv1alpha1.TimeChaos{}
	tc.Name = name
	tc.Namespace = cctx.Namespace

	tc.Spec.Mode = chaosv1alpha1.AllMode
	tc.Spec.Selector = chaosv1alpha1.PodSelectorSpec{
		Pods: map[string][]string{
			cctx.Namespace: pods,
		},
	}
	tc.Spec.TimeOffset = offset

	if err := cctx.Generic.Create(cctx, &tc); err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		return cctx.Generic.Delete(ctx, &tc)
	}, nil
}
