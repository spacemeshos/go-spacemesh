package bootstrap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
    "id": 1681092086,
    "epoch": {
        "number": 2,
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
    "id": 2681092086,
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
    "id": 3681092086,
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
    "id": 4681092086,
    "epoch": {
      "number": 3,
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
	types.DefaultTestAddressConfig()

	res := m.Run()
	os.Exit(res)
}

func checkUpdate1(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 1681092086, got.UpdateId)
	require.EqualValues(t, 2, got.Data.Epoch)
	require.EqualValues(t, "0x6fe7c971", got.Data.Beacon.String())
	require.Len(t, got.Data.ActiveSet, 2)
	require.Equal(t, types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"), got.Data.ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"), got.Data.ActiveSet[1].Hash32())
}

func checkUpdate2(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 2681092086, got.UpdateId)
	require.EqualValues(t, 2, got.Data.Epoch)
	require.Equal(t, types.EmptyBeacon, got.Data.Beacon)
	require.Len(t, got.Data.ActiveSet, 2)
	require.Equal(t, types.HexToHash32("e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd"), got.Data.ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("39125fbda7aac3edcef469b2ad9e6465af4350d28f3d953c6c6660e379546988"), got.Data.ActiveSet[1].Hash32())
}

func checkUpdate3(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 3681092086, got.UpdateId)
	require.EqualValues(t, 3, got.Data.Epoch)
	require.EqualValues(t, "0xf70cf90b", got.Data.Beacon.String())
	require.Nil(t, got.Data.ActiveSet)
}

func checkUpdate4(t *testing.T, got *bootstrap.VerifiedUpdate) {
	require.EqualValues(t, 4681092086, got.UpdateId)
	require.EqualValues(t, 3, got.Data.Epoch)
	require.Equal(t, types.EmptyBeacon, got.Data.Beacon)
	require.Len(t, got.Data.ActiveSet, 3)
	require.Equal(t, types.HexToHash32("65af4350d28f3d953c6c6660e37954698839125fbda7aac3edcef469b2ad9e64"), got.Data.ActiveSet[0].Hash32())
	require.Equal(t, types.HexToHash32("e46b23d64140357b16d18eace600b28ab767bfd7b51c8e9977a342b71c3a23dd"), got.Data.ActiveSet[1].Hash32())
	require.Equal(t, types.HexToHash32("85de8823d6a0cd251aa62ce9315459302ea31ce9701531d3677ac8ba548a4210"), got.Data.ActiveSet[2].Hash32())
}

type checkFunc func(*testing.T, *bootstrap.VerifiedUpdate)

func TestLoad(t *testing.T) {
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
				"1681094134-2023-03-18T22-26-13": update1,
			},
			resultFunc: checkUpdate1,
		},
		{
			desc: "recovery latest",
			persisted: map[string]string{
				"3681094134-2023-04-18T22-26-11": update3,
				"1681094134-2023-03-18T22-26-13": update1,
				"2681094134-2023-04-18T22-26-13": update2,
			},
			resultFunc: checkUpdate3,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := bootstrap.DefaultConfig()
			fs := afero.NewMemMapFs()
			persistDir := filepath.Join(cfg.DataDir, bootstrap.DirName)
			for file, update := range tc.persisted {
				path := filepath.Join(persistDir, file)
				require.NoError(t, afero.WriteFile(fs, path, []byte(update), 0o400))
			}
			updater := bootstrap.New(
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
			)
			ch := updater.Subscribe()
			require.NoError(t, updater.Load(context.Background()))
			if len(tc.persisted) > 0 {
				require.Len(t, ch, 1)
				got := <-ch
				require.NotNil(t, got)
				tc.resultFunc(t, got)
			}
		})
	}
}

func TestStartClose(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	fs := afero.NewMemMapFs()
	persistDir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	require.NoError(t, afero.WriteFile(fs, bootstrap.PersistFilename(persistDir, time.Now().Unix()), []byte(update1), 0o400))
	updater := bootstrap.New(
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
	)
	ch := updater.Subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updater.Start(ctx)
	t.Cleanup(updater.Close)

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
	cancel()
}

func TestPrune(t *testing.T) {
	cfg := bootstrap.DefaultConfig()
	cfg.NumToKeep = 2
	fs := afero.NewMemMapFs()
	persistDir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	now := time.Now()
	require.NoError(t, afero.WriteFile(fs, bootstrap.PersistFilename(persistDir, time.Now().Add(-1*time.Hour).Unix()), []byte(update1), 0o400))
	require.NoError(t, afero.WriteFile(fs, bootstrap.PersistFilename(persistDir, now.Unix()), []byte(update2), 0o400))
	files, err := afero.ReadDir(fs, persistDir)
	require.NoError(t, err)
	require.Len(t, files, cfg.NumToKeep)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(update3))
	}))
	defer ts.Close()
	cfg.URL = ts.URL
	updater := bootstrap.New(
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpClient(ts.Client()),
	)
	require.NoError(t, updater.DoIt(context.Background()))
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
			updates:  []string{update1, update2, update3, update4},
			checkers: []checkFunc{checkUpdate1, checkUpdate2, checkUpdate3, checkUpdate4},
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

			ith := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.updates[ith]))
				ith++
			}))
			defer ts.Close()
			cfg := bootstrap.DefaultConfig()
			cfg.URL = ts.URL
			fs := afero.NewMemMapFs()
			updater := bootstrap.New(
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpClient(ts.Client()),
			)
			ch := updater.Subscribe()
			for i, update := range tc.updates {
				require.NoError(t, updater.DoIt(context.Background()))
				if tc.checkers[i] == nil {
					require.Empty(t, ch)
				} else {
					require.Len(t, ch, 1)
					got := <-ch
					require.NotNil(t, got)
					tc.checkers[i](t, got)
					require.NotEmpty(t, got.Persisted)
					data, err := afero.ReadFile(fs, got.Persisted)
					require.NoError(t, err)
					require.Equal(t, []byte(update), data)
				}
			}
		})
	}
}

func TestEmptyResponse(t *testing.T) {
	fs := afero.NewMemMapFs()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	cfg := bootstrap.DefaultConfig()
	cfg.URL = ts.URL
	updater := bootstrap.New(
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpClient(ts.Client()),
	)

	ch := updater.Subscribe()
	require.NoError(t, updater.DoIt(context.Background()))
	require.Empty(t, ch)
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
    "id": 2681092086,
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
    "id": 2681092086,
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
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := bootstrap.DefaultConfig()
			fs := afero.NewMemMapFs()
			path := filepath.Join(cfg.DataDir, bootstrap.DirName, "00001-2023-03-18T22-26-13")
			require.NoError(t, afero.WriteFile(fs, path, []byte(update1), 0o400))

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.update))
			}))
			defer ts.Close()
			cfg.URL = ts.URL
			updater := bootstrap.New(
				bootstrap.WithConfig(cfg),
				bootstrap.WithLogger(logtest.New(t)),
				bootstrap.WithFilesystem(fs),
				bootstrap.WithHttpClient(ts.Client()),
			)

			ch := updater.Subscribe()
			require.NoError(t, updater.Load(context.Background()))
			require.Len(t, ch, 1)
			got := <-ch
			require.NotNil(t, got)
			checkUpdate1(t, got)
			require.ErrorIs(t, updater.DoIt(context.Background()), tc.err)
		})
	}
}

func TestNoNewUpdate(t *testing.T) {
	etag := "etag"
	cached := false
	fs := afero.NewMemMapFs()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		if cached {
			require.Equal(t, etag, r.Header.Get("If-None-Match"))
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Etag", etag)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(update1))
		cached = true
	}))
	defer ts.Close()
	cfg := bootstrap.DefaultConfig()
	cfg.URL = ts.URL
	updater := bootstrap.New(
		bootstrap.WithConfig(cfg),
		bootstrap.WithLogger(logtest.New(t)),
		bootstrap.WithFilesystem(fs),
		bootstrap.WithHttpClient(ts.Client()),
	)

	ch := updater.Subscribe()
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
}
