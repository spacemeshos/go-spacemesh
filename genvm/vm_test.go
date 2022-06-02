package vm

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/require"
)

func newTester(tb testing.TB) *tester {
	return &tester{
		TB:  tb,
		VM:  New(sql.InMemory(), WithLogger(logtest.New(tb))),
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type tester struct {
	testing.TB
	*VM

	rng *rand.Rand

	pks       []ed25519.PrivateKey
	pubs      []ed25519.PublicKey
	addresses []core.Address
	nonces    []core.Nonce
}

func (t *tester) persistent() *tester {
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	require.NoError(t, err)
	t.VM = New(db, WithLogger(logtest.New(t)))
}

func (t *tester) addAccounts(n int) *tester {
	for i := 0; i < n; i++ {
		pub, pk, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		t.pks = append(t.pks, pk)
		t.pubs = append(t.pubs, pub)
		args := wallet.SpawnArguments{}
		copy(args.PublicKey[:], pub)
		t.addresses = append(t.addresses, ComputePrincipal(wallet.TemplateAddress, core.Nonce{}, &args))
		t.nonces = append(t.nonces, core.Nonce{})
	}
	return t
}

func (t *tester) applyGenesis() *tester {
	accounts := make([]core.Account, len(t.pks))
	for i := range accounts {
		accounts[i] = core.Account{
			Address: types.Address(t.addresses[i]),
			Balance: 100000000000000000,
		}
	}
	require.NoError(t, t.VM.ApplyGenesis(accounts))
	return t
}

func (t *tester) nextNonce(i int) core.Nonce {
	nonce := t.nonces[i]
	t.nonces[i].Counter++
	return nonce
}

func (t *tester) spawnWalletAll() [][]byte {
	var rst [][]byte
	for i := 0; i < len(t.addresses); i++ {
		if t.nonces[i].Counter != 0 {
			continue
		}
		rst = append(rst, t.spawnWallet(i))
	}
	return rst
}

func (t *tester) spawnWallet(i int) []byte {
	typ := scale.U8(0)
	method := scale.U8(0)
	payload := wallet.SpawnPayload{GasPrice: 1}
	copy(payload.Arguments.PublicKey[:], t.pubs[i])
	t.nextNonce(i)
	return encodeWalletTx(t, t.pks[i],
		&typ,
		&t.addresses[i], &method, &wallet.TemplateAddress,
		&payload,
	)
}

func (t *tester) randSendWalletN(n int, amount uint64) [][]byte {
	rst := make([][]byte, n)
	for i := range rst {
		rst[i] = t.randSendWallet(amount)
	}
	return rst
}

func (t *tester) randSendWallet(amount uint64) []byte {
	return t.sendWallet(t.rng.Intn(len(t.addresses)), t.rng.Intn(len(t.addresses)), amount)
}

func (t *tester) sendWallet(from, to int, amount uint64) []byte {
	payload := wallet.SpendPayload{}
	payload.Arguments.Destination = t.addresses[to]
	payload.Arguments.Amount = amount
	payload.GasPrice = 1
	payload.Nonce = wallet.Nonce(t.nextNonce(from))

	typ := scale.U8(0)
	method := scale.U8(1)
	return encodeWalletTx(t, t.pks[from],
		&typ,
		&t.addresses[from],
		&method,
		&payload,
	)
}

func encodeWalletTx(tb testing.TB, pk ed25519.PrivateKey, fields ...scale.Encodable) []byte {
	tb.Helper()

	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		require.NoError(tb, err)
	}
	hash := core.Hash(buf.Bytes())

	sig := ed25519.Sign(pk, hash[:])
	var sigfield scale.Signature
	copy(sigfield[:], sig)
	_, err := sigfield.EncodeScale(encoder)
	require.NoError(tb, err)
	return buf.Bytes()
}

func TestSanity(t *testing.T) {
	tt := newTester(t).
		addAccounts(2).applyGenesis().addAccounts(3)

	skipped, err := tt.Apply(types.NewLayerID(1), [][]byte{
		tt.spawnWallet(0),
		tt.spawnWallet(1),
	})
	require.NoError(t, err)
	require.Empty(t, skipped)
	skipped, err = tt.Apply(types.NewLayerID(2), [][]byte{
		tt.sendWallet(0, 2, 100),
	})
	require.NoError(t, err)
	require.Empty(t, skipped)
}

func BenchmarkWallet(b *testing.B) {
	b.Run("Accounts100k/Txs100k", func(b *testing.B) {
		benchmarkWallet(b, 100_000, 100_000)
	})
}

func benchmarkWallet(b *testing.B, accounts, n int) {
	tt := newTester(b).persistent().
		addAccounts(accounts).applyGenesis()
	lid := types.NewLayerID(1)
	skipped, err := tt.Apply(types.NewLayerID(1), tt.spawnWalletAll())
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	var layers [][][]byte
	for i := 0; i < b.N; i++ {
		layers = append(layers, tt.randSendWalletN(n, 10))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for _, txs := range layers {
		lid = lid.Add(1)
		skipped, err := tt.Apply(lid, txs)
		if err != nil {
			b.Fatal(err)
		}
		if skipped != nil {
			b.Fatalf("skipped transactions %v", skipped)
		}
	}
}
