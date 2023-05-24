package activation

import (
	"testing"

	"github.com/golang/mock/gomock"
	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/post/config"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func fuzzPostSetupOpts(t *testing.T) func(*PostSetupOpts, fuzz.Continue) {
	return func(opts *PostSetupOpts, c fuzz.Continue) {
		opts.DataDir = t.TempDir()
		opts.NumUnits = c.Uint32()
		opts.MaxFileSize = c.Uint64()
		opts.ProviderID = -1
		opts.Throttle = c.RandBool()
		opts.Scrypt = config.ScryptParams{
			N: 2 << c.Intn(32),
			R: 1,
			P: 1,
		}
		opts.ComputeBatchSize = c.Uint64()
	}
}

func Fuzz_StartStopSmeshing(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzer := fuzz.NewFromGoFuzz(data).Funcs(fuzzPostSetupOpts(t))

		var coinbase types.Address
		fuzzer.Fuzz(&coinbase)

		var opts PostSetupOpts
		fuzzer.Fuzz(&opts)

		var deleteFiles bool
		fuzzer.Fuzz(&deleteFiles)

		builder := newTestBuilder(t)
		builder.mpost.EXPECT().PrepareInitializer(gomock.Any(), opts).Return(nil)
		builder.mpost.EXPECT().StartSession(gomock.Any()).Return(nil)
		builder.mpost.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).Return(nil, nil, nil).AnyTimes()

		builder.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(nil).AnyTimes()

		if deleteFiles {
			builder.mpost.EXPECT().Reset().Return(nil)
		}

		require.NoError(t, builder.StartSmeshing(coinbase, opts))
		require.NoError(t, builder.StopSmeshing(deleteFiles))
	})
}
