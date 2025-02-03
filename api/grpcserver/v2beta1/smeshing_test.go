package v2beta1

import (
	"context"
	"testing"

	spacemeshv2beta1 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"github.com/stretchr/testify/require"
)

func TestSmeshingService(t *testing.T) {
	svc := NewSmeshingService(testVersion, testCommit)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2beta1.NewSmeshingServiceClient(conn)

	t.Run("smeshing version", func(t *testing.T) {
		version, err := client.Version(context.Background(), &spacemeshv2beta1.SmeshingVersionRequest{})
		require.NoError(t, err)
		require.Equal(t, testVersion, version.Version)
	})

	t.Run("smeshing build", func(t *testing.T) {
		build, err := client.Build(context.Background(), &spacemeshv2beta1.SmeshingBuildRequest{})
		require.NoError(t, err)
		require.Equal(t, testCommit, build.Build)
	})
}
