package presets

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestCanGeneratePOST(t *testing.T) {
	runTest := func(params config.Config) func(t *testing.T) {
		return func(t *testing.T) {
			req := require.New(t)
			ch := make([]byte, 32)
			cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
			goldenATXID := types.ATXID{2, 3, 4}

			opts := params.SMESHING.Opts
			opts.DataDir = t.TempDir()
			opts.MaxFileSize = 4096

			msync := activation.NewMocksyncer(gomock.NewController(t))
			msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() chan struct{} {
				ch := make(chan struct{})
				close(ch)
				return ch
			})
			mgr, err := activation.NewPostSetupManager(types.NodeID{}, params.POST, logtest.New(t), cdb, msync, goldenATXID)
			req.NoError(err)
			req.NoError(mgr.StartSession(context.Background(), opts))

			_, _, err = mgr.GenerateProof(context.Background(), ch)
			req.NoError(err)
		}
	}

	t.Run("fastnet", runTest(fastnet()))

	t.Run("testnet", runTest(testnet()))
}
