package types

import (
	"encoding/json"
	"testing"

	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
)

// Regression test for https://github.com/spacemeshos/go-spacemesh/issues/4386
func Test_Encode_Decode_ProofMessage(t *testing.T) {
	proofMessage := &PoetProofMessage{
		PoetProof: PoetProof{
			MerkleProof: shared.MerkleProof{
				Root: []byte{1, 2, 3},
			},
			LeafCount: 1234,
		},
		PoetServiceID: []byte("poet_id_123456"),
		RoundID:       "1337",
	}
	encoded, err := codec.Encode(proofMessage)
	require.NoError(t, err)
	var pf PoetProofMessage
	err = codec.Decode(encoded, &pf)
	require.NoError(t, err)
}

type testConfig struct {
	PoetServers []PoetServer
}

func Test_Marshal_PoetConfig(t *testing.T) {
	config := testConfig{
		PoetServers: []PoetServer{
			{
				Address: "https://mainnet-poet-0.spacemesh.network",
				Pubkey:  MustBase64FromString("cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="),
			},
			{
				Address: "https://mainnet-poet-1.spacemesh.network",
				Pubkey:  MustBase64FromString("Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="),
			},
			{
				Address: "https://poet-110.spacemesh.network",
				Pubkey:  MustBase64FromString("8Qqgid+37eyY7ik+EA47Nd5TrQjXolbv2Mdgir243No="),
			},
			{
				Address: "https://poet-111.spacemesh.network",
				Pubkey:  MustBase64FromString("caIV0Ym59L3RqbVAL6UrCPwr+z+lwe2TBj57QWnAgtM="),
			},
			{
				Address: "https://poet-112.spacemesh.network",
				Pubkey:  MustBase64FromString("5p/mPvmqhwdvf8U0GVrNq/9IN/HmZj5hCkFLAN04g1E="),
			},
		},
	}

	j, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.Contains(t, string(j), `"address": "https://mainnet-poet-0.spacemesh.network"`)
	require.Contains(t, string(j), `"pubkey": "cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="`)
	require.Contains(t, string(j), `"address": "https://mainnet-poet-1.spacemesh.network"`)
	require.Contains(t, string(j), `"pubkey": "Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="`)
	require.Contains(t, string(j), `"address": "https://poet-110.spacemesh.network"`)
	require.Contains(t, string(j), `"pubkey": "8Qqgid+37eyY7ik+EA47Nd5TrQjXolbv2Mdgir243No="`)
	require.Contains(t, string(j), `"address": "https://poet-111.spacemesh.network"`)
	require.Contains(t, string(j), `"pubkey": "caIV0Ym59L3RqbVAL6UrCPwr+z+lwe2TBj57QWnAgtM="`)
	require.Contains(t, string(j), `"address": "https://poet-112.spacemesh.network"`)
	require.Contains(t, string(j), `"pubkey": "5p/mPvmqhwdvf8U0GVrNq/9IN/HmZj5hCkFLAN04g1E="`)
}

func Test_Unmarshal_PoetConfig(t *testing.T) {
	config := `{
		"PoetServers": [
			{
			"address": "https://mainnet-poet-0.spacemesh.network",
			"pubkey": "cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="
			},
			{
			"address": "https://mainnet-poet-1.spacemesh.network",
			"pubkey": "Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="
			},
			{
			"address": "https://poet-110.spacemesh.network",
			"pubkey": "8Qqgid+37eyY7ik+EA47Nd5TrQjXolbv2Mdgir243No="
			},
			{
			"address": "https://poet-111.spacemesh.network",
			"pubkey": "caIV0Ym59L3RqbVAL6UrCPwr+z+lwe2TBj57QWnAgtM="
			},
			{
			"address": "https://poet-112.spacemesh.network",
			"pubkey": "5p/mPvmqhwdvf8U0GVrNq/9IN/HmZj5hCkFLAN04g1E="
			}
		]
	}`

	c := testConfig{}
	require.NoError(t, json.Unmarshal([]byte(config), &c))
	require.Len(t, c.PoetServers, 5)
	require.Equal(t, "https://mainnet-poet-0.spacemesh.network", c.PoetServers[0].Address)
	require.Equal(t, MustBase64FromString("cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="), c.PoetServers[0].Pubkey)
	require.Equal(t, "https://mainnet-poet-1.spacemesh.network", c.PoetServers[1].Address)
	require.Equal(t, MustBase64FromString("Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="), c.PoetServers[1].Pubkey)
	require.Equal(t, "https://poet-110.spacemesh.network", c.PoetServers[2].Address)
	require.Equal(t, MustBase64FromString("8Qqgid+37eyY7ik+EA47Nd5TrQjXolbv2Mdgir243No="), c.PoetServers[2].Pubkey)
	require.Equal(t, "https://poet-111.spacemesh.network", c.PoetServers[3].Address)
	require.Equal(t, MustBase64FromString("caIV0Ym59L3RqbVAL6UrCPwr+z+lwe2TBj57QWnAgtM="), c.PoetServers[3].Pubkey)
	require.Equal(t, "https://poet-112.spacemesh.network", c.PoetServers[4].Address)
	require.Equal(t, MustBase64FromString("5p/mPvmqhwdvf8U0GVrNq/9IN/HmZj5hCkFLAN04g1E="), c.PoetServers[4].Pubkey)
}
