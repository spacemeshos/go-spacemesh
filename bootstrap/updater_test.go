package bootstrap_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

const (
	update1 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 1,
    "epochs": [
      {
        "epoch": 0,
        "beacon": "6fe7c971"
      },
      {
        "epoch": 1,
        "beacon": "6fe7c971",
		"activeSet": [
		  "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210",
		  "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"]
      }
    ]
  }
}
`

	update2 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 2,
    "epochs": [
      {
        "epoch": 1,
        "beacon": "6fe7c971",
        "activeSet": [
		  "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210",
		  "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"]
      },
      {
        "epoch": 2,
        "beacon": "f70cf90b",
        "activeSet": [
		  "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
		  "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      }
    ]
  }
}
`

	update3 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 3,
    "epochs": [
      {
        "epoch": 2,
        "beacon": "f70cf90b",
        "activeSet": [
          "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
          "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      },
      {
        "epoch": 3,
        "beacon": "9ef76b65",
        "activeSet": [
          "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64",
          "e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd",
          "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"]
      }
    ]
  }
}
`
)

func TestMain(m *testing.M) {
	types.DefaultTestAddressConfig()

	res := m.Run()
	os.Exit(res)
}

func checkUpdate1(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 1, got.ID)
	require.Len(t, got.Data, 2)
	require.EqualValues(t, got.Data[0].Epoch, 0)
	require.EqualValues(t, "0x6fe7c971", got.Data[0].Beacon.String())
	require.Empty(t, got.Data[0].ActiveSet)
	require.EqualValues(t, got.Data[1].Epoch, 1)
	require.EqualValues(t, "0x6fe7c971", got.Data[1].Beacon.String())
	require.Len(t, got.Data[1].ActiveSet, 2)
	require.Equal(t, types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"), got.Data[1].ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"), got.Data[1].ActiveSet[1].Hash32())
}

func checkUpdate2(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 2, got.ID)
	require.Len(t, got.Data, 2)
	require.EqualValues(t, got.Data[0].Epoch, 1)
	require.EqualValues(t, "0x6fe7c971", got.Data[0].Beacon.String())
	require.Len(t, got.Data[0].ActiveSet, 2)
	require.Equal(t, types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"), got.Data[0].ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"), got.Data[0].ActiveSet[1].Hash32())
	require.EqualValues(t, got.Data[1].Epoch, 2)
	require.EqualValues(t, "0xf70cf90b", got.Data[1].Beacon.String())
	require.Len(t, got.Data[1].ActiveSet, 2)
	require.Equal(t, types.HexToHash32("0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef"), got.Data[1].ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"), got.Data[1].ActiveSet[1].Hash32())
}

func checkUpdate3(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 3, got.ID)
	require.Len(t, got.Data, 2)
	require.EqualValues(t, got.Data[0].Epoch, 2)
	require.EqualValues(t, "0xf70cf90b", got.Data[0].Beacon.String())
	require.Len(t, got.Data[0].ActiveSet, 2)
	require.Equal(t, types.HexToHash32("0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef"), got.Data[0].ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"), got.Data[0].ActiveSet[1].Hash32())
	require.EqualValues(t, got.Data[1].Epoch, 3)
	require.EqualValues(t, "0x9ef76b65", got.Data[1].Beacon.String())
	require.Len(t, got.Data[1].ActiveSet, 3)
	require.Equal(t, types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"), got.Data[1].ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd"), got.Data[1].ActiveSet[1].Hash32())
	require.Equal(t, types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"), got.Data[1].ActiveSet[2].Hash32())
}

type checkFunc func(*testing.T, *bootstrap.VerifiedUpdate)

func TestNew(t *testing.T) {
	tcs := []struct {
		desc       string
		resultFunc checkFunc
		persisted  map[string]string
	}{
		{
			desc: "no recovery",
		},
		{
			desc: "recovery one",
			persisted: map[string]string{
				"00001-2023-03-18T22-26-13": update1,
			},
			resultFunc: checkUpdate1,
		},
		{
			desc: "recovery latest",
			persisted: map[string]string{
				"00014-2023-04-18T22-26-11": update3,
				"00001-2023-03-18T22-26-13": update1,
				"00002-2023-04-18T22-26-13": update2,
			},
			resultFunc: checkUpdate3,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := bootstrap.DefaultConfig()
			ctrl := gomock.NewController(t)
			r1 := bootstrap.NewMockReceiver(ctrl)
			r2 := bootstrap.NewMockReceiver(ctrl)
			receivers := []bootstrap.Receiver{r1, r2}
			if len(tc.persisted) > 0 {
				var received *bootstrap.VerifiedUpdate
				for _, r := range []*bootstrap.MockReceiver{r1, r2} {
					r.EXPECT().OnBoostrapUpdate(gomock.Any()).Do(
						func(got *bootstrap.VerifiedUpdate) {
							tc.resultFunc(t, got)
							if received == nil {
								received = got
							} else {
								require.EqualValues(t, received, got)
							}
						})
				}
			}
			fs := afero.NewMemMapFs()
			persistDir := filepath.Join(cfg.DataDir, bootstrap.DirName)
			for file, update := range tc.persisted {
				path := filepath.Join(persistDir, file)
				require.NoError(t, afero.WriteFile(fs, path, []byte(update), 0o400))
			}
			_, err := bootstrap.New(
				receivers,
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
			)
			require.NoError(t, err)
		})
	}
}

func TestPrune(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	cfg.NumToKeep = 2
	fs := afero.NewMemMapFs()
	persistDir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	require.NoError(t, afero.WriteFile(fs, bootstrap.PersistFilename(persistDir, uint32(1)), []byte(update1), 0o400))
	require.NoError(t, afero.WriteFile(fs, bootstrap.PersistFilename(persistDir, uint32(2)), []byte(update2), 0o400))
	files, err := afero.ReadDir(fs, persistDir)
	require.NoError(t, err)
	require.Len(t, files, cfg.NumToKeep)
	mockhttp := bootstrap.NewMockhttpclient(gomock.NewController(t))
	updater, err := bootstrap.New(
		nil,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpclient(mockhttp),
	)
	require.NoError(t, err)
	mockhttp.EXPECT().Query(gomock.Any(), gomock.Any()).Return([]byte(update3), nil)
	require.NoError(t, updater.DoIt())
	files, err = afero.ReadDir(fs, persistDir)
	require.NoError(t, err)
	require.Len(t, files, cfg.NumToKeep)
}

func TestManyUpdates(t *testing.T) {
	tcs := []struct {
		desc     string
		updates  []string
		checkers []checkFunc
	}{
		{
			desc:     "in order",
			updates:  []string{update1, update2, update3},
			checkers: []checkFunc{checkUpdate1, checkUpdate2, checkUpdate3},
		},
		{
			desc:     "old update number",
			updates:  []string{update2, update1, update3},
			checkers: []checkFunc{checkUpdate2, nil, checkUpdate3},
		},
		{
			desc:     "same updates",
			updates:  []string{update3, update3, update3},
			checkers: []checkFunc{checkUpdate3, nil, nil},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := bootstrap.DefaultConfig()
			fs := afero.NewMemMapFs()
			ctrl := gomock.NewController(t)
			r1 := bootstrap.NewMockReceiver(ctrl)
			r2 := bootstrap.NewMockReceiver(ctrl)
			receivers := []bootstrap.Receiver{r1, r2}
			mockhttp := bootstrap.NewMockhttpclient(ctrl)
			updater, err := bootstrap.New(
				receivers,
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpclient(mockhttp),
			)
			require.NoError(t, err)
			expectedURL, err := url.Parse(bootstrap.DefaultURI)
			require.NoError(t, err)
			for i, update := range tc.updates {
				mockhttp.EXPECT().Query(gomock.Any(), expectedURL).Return([]byte(update), nil)
				var received *bootstrap.VerifiedUpdate
				if tc.checkers[i] != nil {
					for _, r := range []*bootstrap.MockReceiver{r1, r2} {
						r.EXPECT().OnBoostrapUpdate(gomock.Any()).Do(
							func(got *bootstrap.VerifiedUpdate) {
								tc.checkers[i](t, got)
								if received == nil {
									received = got
								} else {
									require.EqualValues(t, received, got)
								}
							})
					}
				}
				require.NoError(t, updater.DoIt())
				if tc.checkers[i] != nil {
					require.NotNil(t, received)
					require.NotEmpty(t, received.Persisted)
					data, err := afero.ReadFile(fs, received.Persisted)
					require.NoError(t, err)
					require.Equal(t, []byte(update), data)
				}
			}
		})
	}
}

func TestGetInvalidUpdate(t *testing.T) {
	tcs := []struct {
		desc   string
		err    error
		update string
	}{
		{
			desc: "different version",
			err:  bootstrap.ErrWrongVersion,
			update: `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.1",
  "data": {
    "id": 2,
    "epochs": [{
        "epoch": 2,
        "beacon": "f70cf90b",
        "activeSet": [
			"0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
			"23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      }
    ]
  }
}
`,
		},
		{
			desc: "epoch out of order",
			err:  bootstrap.ErrEpochOutOfOrder,
			update: `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 2,
    "epochs": [
      {
        "epoch": 3,
        "beacon": "6fe7c971",
        "activeSet": [
		  "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210",
		  "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"]
      },
      {
        "epoch": 2,
        "beacon": "f70cf90b",
        "activeSet": [
		  "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
		  "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      }
    ]
  }
}
`,
		},
		{
			desc: "missing beacon",
			err:  bootstrap.ErrInvalidBeacon,
			update: `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "id": 2,
    "epochs": [{
        "epoch": 2,
        "activeSet": [
			"0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
			"23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
      }
    ]
  }
}
`,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := bootstrap.DefaultConfig()
			ctrl := gomock.NewController(t)
			r := bootstrap.NewMockReceiver(ctrl)
			r.EXPECT().OnBoostrapUpdate(gomock.Any()).Do(
				func(got *bootstrap.VerifiedUpdate) {
					checkUpdate1(t, got)
				})
			fs := afero.NewMemMapFs()
			mockhttp := bootstrap.NewMockhttpclient(ctrl)
			path := filepath.Join(cfg.DataDir, bootstrap.DirName, "00001-2023-03-18T22-26-13")
			require.NoError(t, afero.WriteFile(fs, path, []byte(update1), 0o400))

			updater, err := bootstrap.New(
				[]bootstrap.Receiver{r},
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpclient(mockhttp),
			)
			require.NoError(t, err)

			expectedURL, err := url.Parse(bootstrap.DefaultURI)
			require.NoError(t, err)
			mockhttp.EXPECT().Query(gomock.Any(), expectedURL).Return([]byte(tc.update), nil)
			require.ErrorIs(t, updater.DoIt(), tc.err)
		})
	}
}
