package checkpoint_test

import (
	"bytes"
	_ "embed"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
)

//go:embed checkpointdata.json
var checkpointData string

func TestValidateSchema(t *testing.T) {
	tcs := []struct {
		desc string
		fail bool
		data string
	}{
		{
			desc: "valid",
			data: checkpointData,
		},
		{
			desc: "missing atx",
			fail: true,
			data: `
{"version":"https://spacemesh.io/checkpoint.schema.json.1.0",
"data":{
  "id":"snapshot-15-restore-18",
  "accounts":[{
    "address":"00000000073af7bec018e8d2e379fa47df6a9fa07a6a8344",
    "balance":100000000000000000,
    "nonce":0,
    "template":"",
    "state":""
  }]}
`,
		},
		{
			desc: "missing accounts",
			fail: true,
			data: `
{"version":"https://spacemesh.io/checkpoint.schema.json.1.0",
"data":{
  "id":"snapshot-15-restore-18",
  "atxs":[{
    "id":"d1ef13c8deb8970c19af780f6ce8bbdabfad368afe219ed052fde6766e121cbb",
    "epoch":3,
    "commitmentAtx":"c146f83c2b0f670c7e34e30699536a60b6d5eb13d8f0b63adb6084872c4f3b8d",
    "vrfNonce":144,
    "numUnits":2,
    "baseTickHeight":12401,
    "tickCount":6183,
    "publicKey":"0655283aa44b67e7dcbde46be857334033f5b9af79ec269f45e8e57e7913ed21",
    "sequence":2,
    "coinbase":"000000003100000000000000000000000000000000000000"
  }]}
`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			err := checkpoint.ValidateSchema([]byte(tc.data))
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRecoveryFile(t *testing.T) {
	tcs := []struct {
		desc   string
		fname  string
		exists bool
		expErr string
	}{
		{
			desc:  "all good",
			fname: filepath.Join(t.TempDir(), "test"),
		},
		{
			desc:   "file already exist",
			fname:  filepath.Join(t.TempDir(), "test"),
			exists: true,
			expErr: "file already exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			if tc.exists {
				require.NoError(t, afero.WriteFile(fs, tc.fname, []byte("blah"), 0o600))
			}
			rf, err := checkpoint.NewRecoveryFile(fs, tc.fname)
			if len(tc.expErr) > 0 {
				require.Nil(t, rf)
				require.ErrorContains(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.NoError(t, rf.Copy(fs, bytes.NewReader([]byte(checkpointData))))
			require.NoError(t, rf.Save(fs))
		})
	}
}

func TestRecoveryFile_Copy(t *testing.T) {
	fs := afero.NewMemMapFs()
	rf, err := checkpoint.NewRecoveryFile(fs, filepath.Join(t.TempDir(), "test"))
	require.NoError(t, err)
	require.ErrorContains(t, rf.Copy(fs, bytes.NewReader([]byte{})), "no recovery data")
}

func TestCopyFile(t *testing.T) {
	aferoFS := afero.NewMemMapFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "test_src")
	dst := filepath.Join(dir, "test_dest")
	err := checkpoint.CopyFile(aferoFS, src, dst)
	require.ErrorIs(t, err, fs.ErrNotExist)

	// create src file
	require.NoError(t, afero.WriteFile(aferoFS, src, []byte("blah"), 0o600))
	err = checkpoint.CopyFile(aferoFS, src, dst)
	require.NoError(t, err)

	// dst file cannot be copied over
	err = checkpoint.CopyFile(aferoFS, src, dst)
	require.ErrorIs(t, err, fs.ErrExist)
}
