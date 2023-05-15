package checkpoint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const checkpointdata = `
{"version":"https://spacemesh.io/checkpoint.schema.json.1.0",
"data":{
  "id":"snapshot-15-restore-18",
  "restore":18,
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
  },{
    "id":"fcaac088afd1872cf2b4ab8f1e4c1702b3f184c4c85ead0911f6917222324cf6",
    "epoch":2,
    "commitmentAtx":"c146f83c2b0f670c7e34e30699536a60b6d5eb13d8f0b63adb6084872c4f3b8d",
    "vrfNonce":144,
    "numUnits":2,
    "baseTickHeight":6202,
    "tickCount":6199,
    "publicKey":"0655283aa44b67e7dcbde46be857334033f5b9af79ec269f45e8e57e7913ed21",
    "sequence":1,
    "coinbase":"000000003100000000000000000000000000000000000000"
  }],
  "accounts":[{
    "address":"00000000073af7bec018e8d2e379fa47df6a9fa07a6a8344",
    "balance":100000000000000000,
    "nonce":0,
    "template":"",
    "state":""
  },{
    "address":"000000000dc43c7311d28e000130edbcffd6dc230fd1542e",
    "balance":99999999999863378,
    "nonce":2,
    "template":"000000000000000000000000000000000000000000000001",
    "state":""
  }]}
}`

func Test_ValidateSchema(t *testing.T) {
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
			desc: "missing restore layer",
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
  }],
  "accounts":[{
    "address":"00000000073af7bec018e8d2e379fa47df6a9fa07a6a8344",
    "balance":100000000000000000,
    "nonce":0,
    "template":"",
    "state":""
  }]}
}
`,
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

			err := ValidateSchema([]byte(tc.data))
			if tc.fail {
				require.Error(t, err)
			} else {
				require.Nil(t, err)
			}
		})
	}
}
