package bootstrap_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

const (
	current = types.EpochID(3)
	update1 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "epoch": {
        "number": 1,
        "beacon": "6fe7c971",
		"activeSet": [
		  "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210",
		  "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"]
    }
  }
}
`

	update2 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "epoch": {
        "number": 2,
        "beacon": "00000000",
		"activeSet": [
		  "e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd",
		  "39125fbda7aac3edcef469b2ad9e6465af4350d28f3d953c6c6660e379546988"]
    }
  }
}
`

	update3 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "epoch": {
	  "number": 3,
      "beacon": "f70cf90b",
	  "activeSet": null
	}
  }
}
`

	update4 = `
{
  "version": "https://spacemesh.io/bootstrap.schema.json.1.0",
  "data": {
    "epoch": {
      "number": 4,
      "beacon": "00000000",
      "activeSet": [
        "65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64",
        "e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd",
        "85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"]
    }
  }
}
`
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(4)
	res := m.Run()
	os.Exit(res)
}

func checkUpdate1(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 1, got.Data.Epoch)
	require.EqualValues(t, "0x6fe7c971", got.Data.Beacon.String())
	require.Len(t, got.Data.ActiveSet, 2)
	require.Equal(
		t,
		types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"),
		got.Data.ActiveSet[0].Hash32(),
	)
	require.Equal(
		t,
		types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"),
		got.Data.ActiveSet[1].Hash32(),
	)
}

func checkUpdate2(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 2, got.Data.Epoch)
	require.Equal(t, types.EmptyBeacon, got.Data.Beacon)
	require.Len(t, got.Data.ActiveSet, 2)
	require.Equal(
		t,
		types.HexToHash32("e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd"),
		got.Data.ActiveSet[0].Hash32(),
	)
	require.Equal(
		t,
		types.HexToHash32("39125fbda7aac3edcef469b2ad9e6465af4350d28f3d953c6c6660e379546988"),
		got.Data.ActiveSet[1].Hash32(),
	)
}

func checkUpdate3(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 3, got.Data.Epoch)
	require.EqualValues(t, "0xf70cf90b", got.Data.Beacon.String())
	require.Nil(t, got.Data.ActiveSet)
}

func checkUpdate4(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 4, got.Data.Epoch)
	require.Equal(t, types.EmptyBeacon, got.Data.Beacon)
	require.Len(t, got.Data.ActiveSet, 3)
	require.Equal(
		t,
		types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"),
		got.Data.ActiveSet[0].Hash32(),
	)
	require.Equal(
		t,
		types.HexToHash32("e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd"),
		got.Data.ActiveSet[1].Hash32(),
	)
	require.Equal(
		t,
		types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"),
		got.Data.ActiveSet[2].Hash32(),
	)
}

type checkFunc func(*testing.T, *bootstrap.VerifiedUpdate)

func TestLoad(t *testing.T) {
	tcs := []struct {
		desc        string
		resultFuncs []checkFunc
		persisted   map[types.EpochID][]string
		cached      map[types.EpochID]string
	}{
		{
			desc: "no recovery",
		},
		{
			desc: "recovery required",
			persisted: map[types.EpochID][]string{
				current - 2: {bootstrap.SuffixBootstrap, update1},
				current - 1: {bootstrap.SuffixActiveSet, update2},
				current:     {bootstrap.SuffixBeacon, update3},
				current + 1: {bootstrap.SuffixActiveSet, update4},
			},
			cached: map[types.EpochID]string{
				current - 1: bootstrap.SuffixActiveSet,
				current:     bootstrap.SuffixBeacon,
				current + 1: bootstrap.SuffixActiveSet,
			},
			resultFuncs: []checkFunc{checkUpdate2, checkUpdate3, checkUpdate4},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := bootstrap.DefaultConfig()
			fs := afero.NewMemMapFs()
			for epoch, update := range tc.persisted {
				path := filepath.Join(
					bootstrap.PersistFilename(cfg.DataDir, epoch, fmt.Sprintf("update-%s", update[0])),
				)
				require.NoError(t, fs.MkdirAll(path, 0o700))
				require.NoError(t, afero.WriteFile(fs, path, []byte(update[1]), 0o400))
			}
			mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
			mc.EXPECT().CurrentLayer().Return(current.FirstLayer())
			updater := bootstrap.New(
				mc,
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
			)
			ch, err := updater.Subscribe()
			require.NoError(t, err)
			require.NoError(t, updater.Load(context.Background()))
			for epoch, suffix := range tc.cached {
				require.True(t, updater.Downloaded(epoch, suffix))
			}
			if len(tc.resultFuncs) > 0 {
				require.Len(t, ch, len(tc.resultFuncs))
				for _, fnc := range tc.resultFuncs {
					got := <-ch
					require.NotNil(t, got)
					fnc(t, got)
				}
			}
		})
	}
}

func TestLoadedNotDownloadedAgain(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Failf(t, "should not have queried for update %s", r.URL.String())
	}))
	defer ts.Close()
	cfg := bootstrap.DefaultConfig()
	cfg.URL = ts.URL
	fs := afero.NewMemMapFs()
	persisted := map[types.EpochID]string{
		current - 1: update2,
		current:     update3,
		current + 1: update4,
	}
	for epoch, update := range persisted {
		persisted := filepath.Join(
			cfg.DataDir,
			bootstrap.DirName,
			strconv.Itoa(int(epoch)),
			bootstrap.UpdateName(epoch, bootstrap.SuffixBootstrap),
		)
		require.NoError(t, fs.MkdirAll(filepath.Dir(persisted), 0o700))
		require.NoError(t, afero.WriteFile(fs, persisted, []byte(update), 0o400))
	}
	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(current.FirstLayer()).AnyTimes()
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
	)
	ch, err := updater.Subscribe()
	require.NoError(t, err)
	require.NoError(t, updater.Load(context.Background()))
	require.Len(t, ch, 3)
	for i := 0; i < 3; i++ {
		got := <-ch
		require.NotNil(t, got)
	}
	require.NoError(t, updater.DoIt(context.Background()))
	require.Empty(t, ch)
}

func TestStartClose(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	fs := afero.NewMemMapFs()
	persisted := bootstrap.PersistFilename(cfg.DataDir, current, "bs")
	require.NoError(t, fs.MkdirAll(filepath.Dir(persisted), 0o700))
	require.NoError(t, afero.WriteFile(fs, persisted, []byte(update1), 0o400))
	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(current.FirstLayer()).AnyTimes()
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
	)
	ch, err := updater.Subscribe()
	require.NoError(t, err)
	require.NoError(t, updater.Start())
	defer updater.Close()

	var got *bootstrap.VerifiedUpdate
	require.Eventually(t, func() bool {
		select {
		case got = <-ch:
			return true
		default:
			return false
		}
	}, time.Second, 100*time.Millisecond)
	checkUpdate1(t, got)
	require.NoError(t, updater.Close())
}

func TestPrune(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	fs := afero.NewMemMapFs()
	bsDir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	for _, epoch := range []types.EpochID{current - 2, current - 1, current, current + 1} {
		persisted := bootstrap.PersistFilename(cfg.DataDir, epoch, "bs")
		require.NoError(t, fs.MkdirAll(filepath.Dir(persisted), 0o700))
		require.NoError(t, afero.WriteFile(fs, persisted, []byte(update1), 0o400))
	}
	files, err := afero.ReadDir(fs, bsDir)
	require.NoError(t, err)
	require.Len(t, files, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(update3))
	}))
	defer ts.Close()
	cfg.URL = ts.URL
	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(current.FirstLayer())
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpClient(ts.Client()),
	)
	require.NoError(t, updater.DoIt(context.Background()))
	files, err = afero.ReadDir(fs, bsDir)
	require.NoError(t, err)
	require.Len(t, files, 3)
}

func TestDoIt(t *testing.T) {
	tcs := []struct {
		desc     string
		updates  map[string]string // map server url to contents
		expected []string
		checkers []checkFunc
	}{
		{
			desc: "in order",
			updates: map[string]string{
				"/" + bootstrap.UpdateName(1, bootstrap.SuffixBootstrap): update1,
				"/" + bootstrap.UpdateName(2, bootstrap.SuffixActiveSet): update2,
				"/" + bootstrap.UpdateName(3, bootstrap.SuffixBeacon):    update3,
				"/" + bootstrap.UpdateName(4, bootstrap.SuffixActiveSet): update4,
			},
			expected: []string{update2, update3, update4},
			checkers: []checkFunc{checkUpdate2, checkUpdate3, checkUpdate4},
		},
		{
			desc: "bootstrap trumps others",
			updates: map[string]string{
				"/" + bootstrap.UpdateName(3, bootstrap.SuffixBootstrap): update1,
				"/" + bootstrap.UpdateName(3, bootstrap.SuffixActiveSet): update2,
				"/" + bootstrap.UpdateName(3, bootstrap.SuffixBeacon):    update3,
				"/" + bootstrap.UpdateName(4, bootstrap.SuffixActiveSet): update4,
			},
			expected: []string{update1, update4},
			checkers: []checkFunc{checkUpdate1, checkUpdate4},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				contents, ok := tc.updates[r.URL.String()]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(contents))
			}))
			defer ts.Close()
			cfg := bootstrap.DefaultConfig()
			cfg.URL = ts.URL
			fs := afero.NewMemMapFs()
			mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
			mc.EXPECT().CurrentLayer().Return(current.FirstLayer())
			updater := bootstrap.New(
				mc,
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpClient(ts.Client()),
			)
			ch, err := updater.Subscribe()
			require.NoError(t, err)
			require.NoError(t, updater.DoIt(context.Background()))
			require.Len(t, ch, len(tc.checkers))
			for i, checker := range tc.checkers {
				got := <-ch
				require.NotNil(t, got)
				checker(t, got)
				require.NotEmpty(t, got.Persisted)
				data, err := afero.ReadFile(fs, got.Persisted)
				require.NoError(t, err)
				require.Equal(t, []byte(tc.expected[i]), data)
			}
		})
	}
}

func TestEmptyResponse(t *testing.T) {
	fs := afero.NewMemMapFs()
	numQ := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		numQ++
	}))
	defer ts.Close()
	cfg := bootstrap.DefaultConfig()
	cfg.URL = ts.URL
	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(current.FirstLayer())
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpClient(ts.Client()),
	)

	ch, err := updater.Subscribe()
	require.NoError(t, err)
	require.NoError(t, updater.DoIt(context.Background()))
	require.Empty(t, ch)
	require.Equal(t, 9, numQ)
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
    "epoch": {
	  "number": 2,
      "beacon": "f70cf90b",
      "activeSet": [
	    "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
	    "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
    }
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
    "epoch": {
	  "number": 2,
      "activeSet": [
	    "0575fc4083eb5b5c4422063c87071eb5123d4db6fee7bc1ecb02e52e97916aef",
	    "23716e2667034edc62595a6d1628ff5c323cf099f2cc161e5653a96c9fd2bd55"]
    }
  }
}
`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.update))
			}))
			cfg := bootstrap.DefaultConfig()
			fs := afero.NewMemMapFs()
			defer ts.Close()
			cfg.URL = ts.URL
			mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
			mc.EXPECT().CurrentLayer().Return(current.FirstLayer()).AnyTimes()
			updater := bootstrap.New(
				mc,
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpClient(ts.Client()),
			)

			ch, err := updater.Subscribe()
			require.NoError(t, err)
			require.ErrorIs(t, updater.DoIt(context.Background()), tc.err)
			require.Empty(t, ch)
		})
	}
}

func TestNoNewUpdate(t *testing.T) {
	fs := afero.NewMemMapFs()
	numQ := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		if r.URL.String() != "/"+bootstrap.UpdateName(3, bootstrap.SuffixBootstrap) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(update1))
		numQ++
	}))
	defer ts.Close()
	cfg := bootstrap.DefaultConfig()
	cfg.URL = ts.URL
	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(current.FirstLayer()).AnyTimes()
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpClient(ts.Client()),
	)

	ch, err := updater.Subscribe()
	require.NoError(t, err)
	require.NoError(t, updater.DoIt(context.Background()))
	require.Len(t, ch, 1)
	got := <-ch
	require.NotNil(t, got)
	checkUpdate1(t, got)
	require.NotEmpty(t, got.Persisted)
	data, err := afero.ReadFile(fs, got.Persisted)
	require.NoError(t, err)
	require.Equal(t, []byte(update1), data)

	// no new update
	require.NoError(t, updater.DoIt(context.Background()))
	require.Empty(t, ch)
	require.Equal(t, 1, numQ)
}

func TestRequiredEpochs(t *testing.T) {
	require.Greater(t, types.GetLayersPerEpoch(), uint32(1))
	tcs := []struct {
		desc                string
		exp                 []string
		current, newGenesis uint32
	}{
		{
			desc:    "epoch 0",
			current: types.GetLayersPerEpoch() - 1,
		},
		{
			desc:    "epoch 1",
			current: types.GetLayersPerEpoch(),
			exp: []string{
				"/epoch-2-update-bs",
				"/epoch-2-update-bc",
				"/epoch-2-update-as",
			},
		},
		{
			desc:    "epoch 2",
			current: types.GetLayersPerEpoch() * 2,
			exp: []string{
				"/epoch-2-update-bs",
				"/epoch-2-update-bc",
				"/epoch-2-update-as",
				"/epoch-3-update-bs",
				"/epoch-3-update-bc",
				"/epoch-3-update-as",
			},
		},
		{
			desc:    "past genesis",
			current: types.GetLayersPerEpoch()*10 + 1,
			exp: []string{
				"/epoch-9-update-bs",
				"/epoch-9-update-bc",
				"/epoch-9-update-as",
				"/epoch-10-update-bs",
				"/epoch-10-update-bc",
				"/epoch-10-update-as",
				"/epoch-11-update-bs",
				"/epoch-11-update-bc",
				"/epoch-11-update-as",
			},
		},
		{
			desc:       "checkpoint genesis",
			newGenesis: types.GetLayersPerEpoch() * 10,
			current:    types.GetLayersPerEpoch()*10 + 1,
			exp: []string{
				"/epoch-10-update-bs",
				"/epoch-10-update-bc",
				"/epoch-10-update-as",
				"/epoch-11-update-bs",
				"/epoch-11-update-bc",
				"/epoch-11-update-as",
			},
		},
		{
			desc:       "past checkpoint",
			newGenesis: types.GetLayersPerEpoch() * 10,
			current:    types.GetLayersPerEpoch() * 11,
			exp: []string{
				"/epoch-10-update-bs",
				"/epoch-10-update-bc",
				"/epoch-10-update-as",
				"/epoch-11-update-bs",
				"/epoch-11-update-bc",
				"/epoch-11-update-as",
				"/epoch-12-update-bs",
				"/epoch-12-update-bc",
				"/epoch-12-update-as",
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			var queried []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				queried = append(queried, r.URL.String())
				w.WriteHeader(http.StatusNotFound)
			}))
			defer ts.Close()
			cfg := bootstrap.DefaultConfig()
			cfg.URL = ts.URL
			mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
			oldGenesis := types.GetEffectiveGenesis()
			if tc.newGenesis > 0 {
				types.SetEffectiveGenesis(tc.newGenesis)
			}
			mc.EXPECT().CurrentLayer().Return(types.LayerID(tc.current))
			updater := bootstrap.New(
				mc,
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpClient(ts.Client()),
			)

			ch, err := updater.Subscribe()
			require.NoError(t, err)
			require.NoError(t, updater.DoIt(context.Background()))
			require.Empty(t, ch)
			require.Equal(t, tc.exp, queried)
			if tc.newGenesis > 0 {
				types.SetEffectiveGenesis(oldGenesis.Uint32())
			}
		})
	}
}

func TestIntegration(t *testing.T) {
	t.Skip("testing against gs bucket. only execute locally")
	cfg := bootstrap.DefaultConfig()
	fs := afero.NewMemMapFs()
	cfg.URL = "https://bootstrap.spacemesh.network/test-only"
	// make sure epoch 53 only has beacon and active set files, and both will be retrieved
	// and epoch 54 has all 3 (bootstrap, beacon and active set files), while only bootstrap file will be retrieved
	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(types.EpochID(53).FirstLayer()).AnyTimes()
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
	)
	ch, err := updater.Subscribe()
	require.NoError(t, err)
	require.NoError(t, updater.DoIt(context.Background()))
	require.Len(t, ch, 3)
	got := <-ch
	require.EqualValues(t, 53, got.Data.Epoch)
	require.NotEqual(t, types.EmptyBeacon, got.Data.Beacon)
	require.Empty(t, got.Data.ActiveSet)
	got = <-ch
	require.EqualValues(t, 53, got.Data.Epoch)
	require.Equal(t, types.EmptyBeacon, got.Data.Beacon)
	require.NotEmpty(t, got.Data.ActiveSet)
	got = <-ch
	require.EqualValues(t, 54, got.Data.Epoch)
	require.NotEqual(t, types.EmptyBeacon, got.Data.Beacon)
	require.NotEmpty(t, got.Data.ActiveSet)

	require.NoError(t, updater.DoIt(context.Background()))
	require.Empty(t, ch)
}

func TestClose(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	fs := afero.NewMemMapFs()

	mc := bootstrap.NewMocklayerClock(gomock.NewController(t))
	updater := bootstrap.New(
		mc,
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
	)
	ch, err := updater.Subscribe()
	require.NoError(t, err)
	require.NotNil(t, ch)

	// close on multiple goroutines without error or race
	var eg errgroup.Group
	for i := 0; i < 10; i++ {
		eg.Go(func() error {
			require.NoError(t, updater.Close())
			return nil
		})
	}

	eg.Wait()

	// subscribe after close
	ch, err = updater.Subscribe()
	require.Error(t, err)
	require.Nil(t, ch)
}
