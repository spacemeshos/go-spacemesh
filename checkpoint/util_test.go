package checkpoint_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
)

const checkpointdata = `{"version":"https://spacemesh.io/checkpoint.schema.json.1.0","data":{"id":"snapshot-15","atxs":[{"id":"mORyeMH1is/StnCnMPKImPdOsUBIKge5H/gfn/C32fQ=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":114,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"AjDF111CuE+YgA7OtHvJzE2AMFiQClA0agn/YdVrZYI=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"e7QJXNX9Xv/HGaWkXbBh7zJLjDAH05LJbn6ETVkaqJM=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":118,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"BEFt4bjPnzI3hKjLqtnTqT0DAZARRHkvtTvWZG5fGWo=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"Ucq8mQbeucxEOwwUjljhuoO+zbqJl5rFQF+oSVYiKM4=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":36,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"DWOFdx7D7nm+pb836Ke59MPfPb21ZHLMqPFwsh6r5TA=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"a3E203WsejzivHgXnnTFo5d+wTs4bXlpuk8C7cUtcVI=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":118,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"DmlnvWmNXbU/spggGtx9Eopqmcgj8XxNrLN9YDPMLs4=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"7zwIpqALLONL+8vgJDxQE+P9W0ObYGpXzJymFmC1HPM=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":225,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"GymS9e2sTLJoHAThUEkYG57LpwESILJIqNL7AgGhR3k=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"m1OXiG6whx9bSgDFZEwRDzVhjYFlUo1jakPd9gP+ix0=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":28,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"JBASUKxLLO/PeKJQRCk+hObdHINqpRm0k/GfwZsGpoU=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"EybL87BLqJPeoOESnohH35v+cwpjKKPIoojCYD1XOyQ=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":169,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"JLJQBNA9gK3mc4YUNeeV7uLxbl1/kU9/Eb4bx40UBRE=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"+NsAaWCfgNnM14YBaiRr0awmIxFEfSbwQDSJY/GHEMY=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":162,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"JL5BCzlK9IKVctKhJuSp2H+bP5iwb4/PBXH5enXud+8=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"8/YqDIi87zSct9QRcEKEsF7H4y/FkbDQuAnaHLDkpSI=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":251,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"Jz2niGSkirbv7nOyGZ3+8heGVbx1q1YdANiljTiCAGQ=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"bxGrD1Nsk/bHeuMrmvLjLBmcbaRYyT2M1q/x1RYYpnA=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":35,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"R1QpJYjIYJSauXfIlWtzPRC/eP2PJMuy+iDEc/wxnFw=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"rIcqVvi+kSuaqF/g47dbYrI9rx993oRsiqkjkvmSuy4=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":163,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"UWcgflkxKJ2mtXopUjmQzfntrlUFP2Qly70Y+13ALFY=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"OOhu1255ZmVPnesTnJ7/Zo4e7k1FqsBWJ4lIRk9PtDk=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":224,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"XkdK5+lrPxDLpmVgwOklWj+zATbPq355uBvfT5+Imrg=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"fpDfLfjSZMhB8fSADJpBwKecaz454cW54UO3QyAIATI=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":58,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"YTtPPMQkuHKGUzOu7OyuJo1gKgFNsGvuebF41EXfq98=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"CxWR7O6gq0PAlkF7sMCKpiFX1m38DPU9o63GqJY1tlI=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":160,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"dQaH3Xu/EmSOD/YsGwERP/b2WSNwNXP3cEmFODF8OKc=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"/4mpsMHE/b/7aaiZjercT32tr+DiYG6ZuusfAjiXYL8=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":175,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"d1V8P6y/q8zzcmTL3oNTEcwp1mRvnY44h4/gMTbrUVE=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"2fVXJwJE1eDCXCwVR+A+Fs6jSkfq9xCG5QHSw+26v1Q=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":66,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"eAg30tcT7v2wAKlImtz9+gGB9RZwOUu/oBRS3CQsDIM=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"pSCDIQkwMJw4lNWmLYoX7AxrFaln42Gx3bl+zWEBJDw=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":63,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"gGO0GpK9K+/jK91DJBJtDz0dAFgeVrvi7CeneExQeW8=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"DNW3RHxjCwbXtcJqvZ2myYQusfI4NPQM+K1oufhkOJA=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":122,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"lDtxQqzy1DPpFJHb/w0I/QKkfST0iF4iULkkYuhLjB0=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"k1eMLxhzJ4XWuxKLED8PNpgWohFIs6bu84qIPGsdwPI=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":111,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"ne6Xsdv9hyMPzm/EFk+iVvHwhe/qIKNlsnJz4+LneMY=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"CUUdaKPTxKbboLrH5JAv9/g8eTdXgrWhomHr8ozxjO0=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":9,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"nuoQ7Ji1PSoe6TWDxea2Wd+Q3zN/CM7UsN6jzLo8E6o=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"p0sG4uz9PgsjocZdge6IcsZk1PEEhCGiuGhZ7WhmX08=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":100,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"tYesWSTOwZMS9NWkGNmNtMSSyONl0aqvuE0RaFnpa2g=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"CTtqOhleEEUjf4PQ/g4cAIUNE88oIuBbkkaXH367mEM=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":100,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"vopmBzDRKMEACuU5/vgwJhLX36KOz26fUXvTpth0Hj8=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"WeYO5DzkmT+LOD+LPkMTpbG36nacu+MkWIFrdn4iAR8=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":187,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"yZgoeiL/ZUh/UK29rZLj/SCuF6oRyBu8qFJlIOYM+FA=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"9R1RfLd9HtlV8dPOgMRbkSXJ4SDXHELrt89UiU9UYww=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":55,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"y6/LmwlUGbzaZ2Hp1x83HBIpG4K8OQOVKAFeT6aQ1g0=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"F5O01qw5QxzJkV5V0MSsAposs732b79qJZbr4Umm2qM=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":184,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"12zcRs1oRwsXdKOrF6p4SPrirOm02R2hwJudh28f1c8=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"},{"id":"sc3vQd4SIonpamSsxl+tksvz94Wljju2FncHGI4GWM8=","epoch":2,"commitmentAtx":"iXlw/UMbcz+h/Bm+l90zA2GnKWVg3dEEOV3fP8vdKfE=","vrfNonce":18,"numUnits":2,"baseTickHeight":6162,"tickCount":6159,"publicKey":"9UMrQ+L5i51d9ik3SQ5202QsOa4AnDWzTj+QQC3mNg4=","sequence":1,"coinbase":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA"}],"accounts":[{"address":"AAAAAAc6977AGOjS43n6R99qn6B6aoNE","balance":100000000000000000,"nonce":0,"template":null,"state":null},{"address":"AAAAAAsBAQAAAAAAAAAAAAAAAAAAAAAA","balance":1000,"nonce":0,"template":null,"state":null},{"address":"AAAAABsxTRfAWikF+RjQ1y8vaYlkD7tD","balance":100000000000000000,"nonce":0,"template":null,"state":null},{"address":"AAAAABuplkSS7uc5nwy4I5Og9PhuwFMS","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"yFk7fs/6xYbisUDOENMsr1Rt3CseVQkcSKA409KnyDE="},{"address":"AAAAADEAAAAAAAAAAAAAAAAAAAAAAAAA","balance":3343327309466,"nonce":0,"template":null,"state":null},{"address":"AAAAAE/bRPsGcLFsZXAvgtq06B0goSxS","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"N5hEV9rrr/0l6EH/jd5RRi4uDv4zJPok9vMVAloCCjo="},{"address":"AAAAAFoCrsQ+F8gKsTPgDIOBVD0IaOHP","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"9u1uvfA/aIeKs3wHABX5v0cuw41e88dT97J/7/rgWys="},{"address":"AAAAAHYI4uxyryMeLuLHHDEHSdNZn+Uc","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"HqsEi7+vZ7oqINxH7lYKDpyQVerJhSvKfhSHDcI6JMg="},{"address":"AAAAAIEtzQqCaLIJQfBXw1DuOPpQZv+y","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"UC65cj7HOPJqYagaeVy3SXy8weDmtvKRVZu+WQYqhXM="},{"address":"AAAAAINzk5402WXIT+3ti89stMmZQiAI","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"CPvzEMLPa3bWbNTo02pqjHyI7PEEZ2lv/lGkYLwuHIs="},{"address":"AAAAALOIq50BKZxlVKEZqONtsNlyOUeJ","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"Gfmjt6Kd+wJN6Pa4hTapZaOYJC7V/9YUodrzkVPYWeg="},{"address":"AAAAAL1XWyitwtnCf75AjQ3alv/cOsTJ","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"epKOw7UakXzPuNQ5UmwrmC5c5VH5J8Wd/OGd++ut4KE="},{"address":"AAAAANmtySRp9InS6YkIFZZLhRARq80t","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"6CGvkwPVkE3oRCtwM/KnS4qrsF37w40z6tE7HsQDD0E="},{"address":"AAAAAPRFrccwTVI1jHzxBoZzFLhZqFYo","balance":99999999999863378,"nonce":2,"template":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB","state":"Q9PuZ7NKMTO5fUIgdCG0tptm7AEUjonCKEeKsAWXALk="}]}}`

func TestValidateSchema(t *testing.T) {
	tcs := []struct {
		desc string
		fail bool
		data string
	}{
		{
			desc: "valid",
			data: checkpointdata,
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
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			err := checkpoint.ValidateSchema([]byte(tc.data))
			if tc.fail {
				require.Error(t, err)
			} else {
				require.Nil(t, err)
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
		tc := tc
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
			require.NoError(t, rf.Copy(fs, bytes.NewReader([]byte(checkpointdata))))
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
	fs := afero.NewMemMapFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "test_src")
	dst := filepath.Join(dir, "test_dest")
	err := checkpoint.CopyFile(fs, src, dst)
	require.ErrorIs(t, err, os.ErrNotExist)

	// create src file
	require.NoError(t, afero.WriteFile(fs, src, []byte("blah"), 0o600))
	err = checkpoint.CopyFile(fs, src, dst)
	require.NoError(t, err)

	// dst file cannot be copied over
	err = checkpoint.CopyFile(fs, src, dst)
	require.ErrorIs(t, err, os.ErrExist)
}
