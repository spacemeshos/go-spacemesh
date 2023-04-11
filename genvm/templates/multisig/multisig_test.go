package multisig

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

func TestVerify(t *testing.T) {
	const keys = 10
	privates := []ed25519.PrivateKey{}
	publics := []ed25519.PublicKey{}
	for i := 0; i < keys; i++ {
		pub, pk, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		privates = append(privates, pk)
		publics = append(publics, pub)
	}
	empty := types.Hash20{}
	message := []byte("message")
	signed := append(empty[:], message...)

	for _, variant := range []struct{ K, N int }{
		{K: 2, N: 3},
		{K: 3, N: 5},
		{K: 4, N: 7},
		{K: 5, N: 9},
	} {
		t.Run(fmt.Sprintf("%d/%d", variant.K, variant.N), func(t *testing.T) {
			for _, tc := range []struct {
				desc      string
				verified  bool
				signature []byte
			}{
				{
					desc:      "empty",
					signature: []byte{},
				},
				{
					desc:      "invalid",
					signature: []byte("test"),
				},
				{
					desc:      "verified seq",
					verified:  true,
					signature: prepSigFromKeys(t, signed, privates, pullSeqRefs(variant.K)...),
				},
				{
					desc:      "verified non seq",
					verified:  true,
					signature: prepSigFromKeys(t, signed, privates, pullNonSeqRefs(variant.K, variant.N, 2)...),
				},
				{
					desc:      "duplicates",
					signature: prepSigFromKeys(t, signed, privates, pullWithDups(variant.K)...),
				},
				{
					desc:      "more than k",
					signature: prepSigFromKeys(t, message, privates, pullSeqRefs(variant.K+1)...),
				},
				{
					desc:      "reverse order",
					signature: prepSigFromKeys(t, signed, privates, pullReverse(variant.K)...),
				},
			} {
				t.Run(tc.desc, func(t *testing.T) {
					ms := MultiSig{
						Required: uint8(variant.K),
					}
					for _, pub := range publics[:variant.N] {
						key := core.PublicKey{}
						copy(key[:], pub)
						ms.PublicKeys = append(ms.PublicKeys, key)
					}
					require.Equal(t, tc.verified,
						ms.Verify(
							&core.Context{},
							append(message, tc.signature...),
							scale.NewDecoder(bytes.NewReader(tc.signature)),
						),
					)
				})
			}
		})
	}
}

func BenchmarkVerify(b *testing.B) {
	const keys = 10
	privates := []ed25519.PrivateKey{}
	publics := []ed25519.PublicKey{}
	for i := 0; i < keys; i++ {
		pub, pk, err := ed25519.GenerateKey(nil)
		require.NoError(b, err)
		privates = append(privates, pk)
		publics = append(publics, pub)
	}
	empty := types.Hash20{}
	message := []byte("message")
	signed := append(empty[:], message...)

	for _, variant := range []struct{ K, N int }{
		{K: 2, N: 3},
		{K: 3, N: 5},
		{K: 4, N: 7},
		{K: 5, N: 9},
	} {
		b.Run(fmt.Sprintf("%d/%d", variant.K, variant.N), func(b *testing.B) {
			ms := MultiSig{
				Required: uint8(variant.K),
			}
			for _, pub := range publics[:variant.N] {
				key := core.PublicKey{}
				copy(key[:], pub)
				ms.PublicKeys = append(ms.PublicKeys, key)
			}
			sig := prepSigFromKeys(b, signed, privates, pullSeqRefs(variant.K)...)
			raw := append(message, sig...)
			dec := scale.NewDecoder(nil)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dec.Reset(bytes.NewReader(sig))
				if !ms.Verify(&core.Context{GenesisID: empty}, raw, dec) {
					b.Fatal("all signatures must be valid")
				}
			}
		})
	}
}

func FuzzVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		ms := MultiSig{Required: 2, PublicKeys: make([]core.PublicKey, 3)}
		dec := scale.NewDecoder(bytes.NewReader(data))
		ms.Verify(&core.Context{}, data, dec)
	})
}

func pullReverse(k int) (rst []int) {
	for i := k - 1; len(rst) > 0; i-- {
		rst = append(rst, i)
	}
	return rst
}

func pullSeqRefs(k int) (rst []int) {
	for i := 0; len(rst) < k; i++ {
		rst = append(rst, i)
	}
	return rst
}

func pullNonSeqRefs(k, n, step int) (rst []int) {
	for i := 0; len(rst) < k && i < n; i += step {
		rst = append(rst, i)
	}
	return rst
}

func pullWithDups(k int) (rst []int) {
	for i := 0; len(rst) < k; i++ {
		rst = append(rst, i)
		if len(rst) < k {
			rst = append(rst, i)
		}
	}
	return rst
}

func prepSigFromKeys(tb testing.TB, message []byte, privates []ed25519.PrivateKey, refs ...int) []byte {
	tb.Helper()
	sigs := Signatures{}
	hash := core.Hash(message)
	for _, ref := range refs {
		sig := core.Signature{}
		copy(sig[:], ed25519.Sign(privates[ref], hash[:]))
		sigs = append(sigs, Part{Ref: uint8(ref), Sig: sig})
	}
	buf := bytes.NewBuffer(nil)
	enc := scale.NewEncoder(buf)
	_, err := scale.EncodeStructArray(enc, sigs[:])
	require.NoError(tb, err)
	return buf.Bytes()
}
