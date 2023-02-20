package vm

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/economics/rewards"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	sdkmultisig "github.com/spacemeshos/go-spacemesh/genvm/sdk/multisig"
	sdkwallet "github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func testContext(lid types.LayerID) ApplyContext {
	return ApplyContext{
		Layer: lid,
	}
}

func newTester(tb testing.TB) *tester {
	return &tester{
		TB:  tb,
		VM:  New(sql.InMemory(), WithLogger(logtest.New(tb))),
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type testAccount interface {
	getAddress() core.Address
	getTemplate() core.Address
	spend(to core.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) []byte
	selfSpawn(nonce core.Nonce, opts ...sdk.Opt) []byte
	foreignCall(target core.Address, method core.Method, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte

	spawn(template core.Address, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte
	spawnArgs() scale.Encodable

	// fixed gas for spawn and spend
	baseGas() int
	spawnGas() int
	spendGas() int
}

type singlesigAccount struct {
	pk      ed25519.PrivateKey
	address core.Address
}

func (a *singlesigAccount) getAddress() core.Address {
	return a.address
}

func (a *singlesigAccount) getTemplate() core.Address {
	return wallet.TemplateAddress
}

func (a *singlesigAccount) spend(to core.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return sdkwallet.Spend(signing.PrivateKey(a.pk), to, amount, nonce, opts...)
}

func (a *singlesigAccount) selfSpawn(nonce core.Nonce, opts ...sdk.Opt) []byte {
	return sdkwallet.SelfSpawn(signing.PrivateKey(a.pk), nonce, opts...)
}

func (a *singlesigAccount) spawn(template core.Address, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return sdkwallet.Spawn(signing.PrivateKey(a.pk), template, args, nonce, opts...)
}

func (a *singlesigAccount) spawnArgs() scale.Encodable {
	args := wallet.SpawnArguments{}
	copy(args.PublicKey[:], signing.Public(signing.PrivateKey(a.pk)))
	return &args
}

func (a *singlesigAccount) baseGas() int {
	return wallet.BaseGas
}

func (a *singlesigAccount) spendGas() int {
	return wallet.BaseGas + wallet.FixedGasSpend
}

func (a *singlesigAccount) spawnGas() int {
	return wallet.BaseGas + wallet.FixedGasSpawn
}

func (a *singlesigAccount) foreignCall(target core.Address, method core.Method, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return sdkwallet.Foreign(signing.PrivateKey(a.pk), target, method, args, nonce, opts...)
}

type multisigAccount struct {
	k        int
	pks      []ed25519.PrivateKey
	address  core.Address
	template core.Address
}

func (a *multisigAccount) getAddress() core.Address {
	return a.address
}

func (a *multisigAccount) getTemplate() core.Address {
	return a.template
}

func (a *multisigAccount) spend(to core.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) []byte {
	agg := sdkmultisig.Spend(0, a.pks[0], a.address, to, amount, nonce, opts...)
	for i := 1; i < a.k; i++ {
		part := sdkmultisig.Spend(uint8(i), a.pks[i], a.address, to, amount, nonce, opts...)
		agg.Add(*part.Part(uint8(i)))
	}
	return agg.Raw()
}

func (a *multisigAccount) selfSpawn(nonce core.Nonce, opts ...sdk.Opt) []byte {
	var pubs []ed25519.PublicKey
	for _, pk := range a.pks {
		pubs = append(pubs, ed25519.PublicKey(signing.Public(signing.PrivateKey(pk))))
	}
	var agg *sdkmultisig.Aggregator
	for i := 0; i < a.k; i++ {
		part := sdkmultisig.SelfSpawn(uint8(i), a.pks[i], a.template, pubs, nonce, opts...)
		if agg == nil {
			agg = part
		} else {
			agg.Add(*part.Part(uint8(i)))
		}
	}
	return agg.Raw()
}

func (a *multisigAccount) spawn(template core.Address, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	agg := sdkmultisig.Spawn(0, a.pks[0], a.address, template, args, nonce, opts...)
	for i := 1; i < a.k; i++ {
		part := sdkmultisig.Spawn(uint8(i), a.pks[i], a.address, template, args, nonce, opts...)
		agg.Add(*part.Part(uint8(i)))
	}
	return agg.Raw()
}

func (a *multisigAccount) foreignCall(target core.Address, method core.Method, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	agg := sdkmultisig.Foreign(0, a.pks[0], a.address, target, method, args, nonce, opts...)
	for i := 1; i < a.k; i++ {
		part := sdkmultisig.Foreign(uint8(i), a.pks[i], a.address, target, method, args, nonce, opts...)
		agg.Add(*part.Part(uint8(i)))
	}
	return agg.Raw()
}

func (a *multisigAccount) spawnArgs() scale.Encodable {
	args := multisig.SpawnArguments{
		PublicKeys: make([]core.PublicKey, len(a.pks)),
	}
	for i, pk := range a.pks {
		copy(args.PublicKeys[i][:], signing.Public(signing.PrivateKey(pk)))
	}
	return &args
}

func (a *multisigAccount) spendGas() int {
	switch a.template {
	case multisig.TemplateAddress1:
		return multisig.BaseGas1 + multisig.FixedGasSpend1
	case multisig.TemplateAddress2:
		return multisig.BaseGas2 + multisig.FixedGasSpend2
	case multisig.TemplateAddress3:
		return multisig.BaseGas3 + multisig.FixedGasSpend3
	}
	panic("unknown template")
}

func (a *multisigAccount) baseGas() int {
	switch a.template {
	case multisig.TemplateAddress1:
		return multisig.BaseGas1
	case multisig.TemplateAddress2:
		return multisig.BaseGas2
	case multisig.TemplateAddress3:
		return multisig.BaseGas3
	}
	panic("unknown template")
}

func (a *multisigAccount) spawnGas() int {
	switch a.template {
	case multisig.TemplateAddress1:
		return multisig.BaseGas1 + multisig.FixedGasSpawn1
	case multisig.TemplateAddress2:
		return multisig.BaseGas2 + multisig.FixedGasSpawn2
	case multisig.TemplateAddress3:
		return multisig.BaseGas3 + multisig.FixedGasSpawn3
	}
	panic("unknown template")
}

type vaultAccount struct {
	address core.Address

	owner                      core.Address
	vestingStart, vestingEnd   core.LayerID
	totalAmount, initialUnlock uint64
}

func (a *vaultAccount) getTemplate() core.Address {
	return vault.TemplateAddress
}

func (a *vaultAccount) getAddress() core.Address {
	return a.address
}

func (a *vaultAccount) spend(to core.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return nil
}

func (a *vaultAccount) selfSpawn(nonce core.Nonce, opts ...sdk.Opt) []byte {
	return nil
}

func (a *vaultAccount) spawn(template core.Address, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return nil
}

func (a *vaultAccount) foreignCall(target core.Address, method core.Method, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return nil
}

func (a *vaultAccount) spawnArgs() scale.Encodable {
	return &vault.SpawnArguments{
		Owner:               a.owner,
		TotalAmount:         a.totalAmount,
		InitialUnlockAmount: a.initialUnlock,
		VestingStart:        a.vestingStart,
		VestingEnd:          a.vestingEnd,
	}
}

func (a *vaultAccount) spawnGas() int {
	return 0
}

func (a *vaultAccount) spendGas() int {
	return vault.FixedGasSpend
}

func (a *vaultAccount) baseGas() int {
	return 0
}

type tester struct {
	testing.TB
	*VM

	rng *rand.Rand

	accounts []testAccount
	nonces   []core.Nonce
}

func (t *tester) persistent() *tester {
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, err)
	t.VM = New(db, WithLogger(logtest.New(t)))
	return t
}

func (t *tester) withGasLimit(limit uint64) *tester {
	t.VM.cfg.GasLimit = limit
	return t
}

func (t *tester) addAccount(account testAccount) {
	t.accounts = append(t.accounts, account)
	t.nonces = append(t.nonces, 0)
}

func (t *tester) addSingleSig(n int) *tester {
	for i := 0; i < n; i++ {
		pub, pk, err := ed25519.GenerateKey(t.rng)
		require.NoError(t, err)
		t.addAccount(&singlesigAccount{pk: pk, address: sdkwallet.Address(pub)})
	}
	return t
}

func (t *tester) createMultisig(k, n int, template core.Address) *multisigAccount {
	pks := []ed25519.PrivateKey{}
	pubs := [][]byte{}
	for j := 0; j < n; j++ {
		pub, pk, err := ed25519.GenerateKey(t.rng)
		require.NoError(t, err)
		pks = append(pks, pk)
		pubs = append(pubs, pub)
	}
	return &multisigAccount{
		k:        k,
		pks:      pks,
		address:  sdkmultisig.Address(template, pubs...),
		template: template,
	}
}

func (t *tester) addMultisig(total, k, n int, template core.Address) *tester {
	for i := 0; i < total; i++ {
		t.addAccount(t.createMultisig(k, n, template))
	}
	return t
}

func (t *tester) addVault(owners int, totalAmount, initialUnlock uint64, start, end types.LayerID) *tester {
	for i := 0; i < owners; i++ {
		account := &vaultAccount{
			owner:         t.accounts[i].getAddress(),
			totalAmount:   totalAmount,
			initialUnlock: initialUnlock,
			vestingStart:  start,
			vestingEnd:    end,
		}
		account.address = core.ComputePrincipal(account.getTemplate(), account.spawnArgs())
		t.addAccount(account)
	}
	return t
}

func (t *tester) applyGenesis() *tester {
	return t.applyGenesisWithBalance(1_000_000_000_000)
}

func (t *tester) applyGenesisWithBalance(amount uint64) *tester {
	accounts := make([]core.Account, len(t.accounts))
	for i := range accounts {
		accounts[i] = core.Account{
			Address: t.accounts[i].getAddress(),
			Balance: amount,
		}
	}
	require.NoError(t, t.VM.ApplyGenesis(accounts))
	return t
}

func (t *tester) nextNonce(i int) core.Nonce {
	nonce := t.nonces[i]
	t.nonces[i]++
	return nonce
}

func (t *tester) spawnAll() []types.RawTx {
	var rst []types.RawTx
	for i := 0; i < len(t.accounts); i++ {
		if t.nonces[i] != 0 {
			continue
		}
		rst = append(rst, t.selfSpawn(i))
	}
	return rst
}

func (t *tester) selfSpawn(i int, opts ...sdk.Opt) types.RawTx {
	nonce := t.nextNonce(i)
	return types.NewRawTx(t.accounts[i].selfSpawn(nonce, opts...))
}

func (t *tester) spawn(i, j int, opts ...sdk.Opt) types.RawTx {
	nonce := t.nextNonce(i)
	return types.NewRawTx(t.accounts[i].spawn(t.accounts[j].getTemplate(), t.accounts[j].spawnArgs(), nonce, opts...))
}

func (t *tester) randSpendN(n int, amount uint64) []types.RawTx {
	rst := make([]types.RawTx, n)
	for i := range rst {
		rst[i] = t.randSpend(amount)
	}
	return rst
}

func (t *tester) randSpend(amount uint64) types.RawTx {
	return t.spend(t.rng.Intn(len(t.accounts)), t.rng.Intn(len(t.accounts)), amount)
}

func (t *tester) withSeed(seed int64) *tester {
	t.rng = rand.New(rand.NewSource(seed))
	return t
}

func (t *tester) spend(from, to int, amount uint64, opts ...sdk.Opt) types.RawTx {
	return t.spendWithNonce(from, to, amount, t.nextNonce(from), opts...)
}

func (t *tester) spendWithNonce(from, to int, amount uint64, nonce core.Nonce, opts ...sdk.Opt) types.RawTx {
	return types.NewRawTx(t.accounts[from].spend(t.accounts[to].getAddress(), amount, nonce, opts...))
}

type reward struct {
	address int
	share   float64
}

func (t *tester) rewards(all ...reward) []types.AnyReward {
	var rst []types.AnyReward
	for _, rew := range all {
		rat := new(big.Rat).SetFloat64(rew.share)
		rst = append(rst, types.AnyReward{
			Coinbase: t.accounts[rew.address].getAddress(),
			Weight: types.RatNum{
				Num:   rat.Num().Uint64(),
				Denom: rat.Denom().Uint64(),
			},
		})
	}
	return rst
}

func (t *tester) estimateSelfSpawnGas(principal int) int {
	return t.accounts[principal].spawnGas() +
		len(t.accounts[principal].selfSpawn(0))*int(t.VM.cfg.StorageCostFactor)
}

func (t *tester) estimateSpawnGas(principal int) int {
	return t.accounts[principal].spawnGas() +
		len(t.accounts[principal].spawn(wallet.TemplateAddress, t.accounts[principal].spawnArgs(), 0))*int(t.VM.cfg.StorageCostFactor)
}

func (t *tester) estimateSpendGas(principal, to, amount int, nonce core.Nonce) int {
	return t.accounts[principal].spendGas() + len(t.accounts[principal].spend(t.accounts[to].getAddress(), uint64(amount), nonce))*int(t.VM.cfg.StorageCostFactor)
}

func (t *tester) estimateDrainGas(principal, vault, to, amount int, nonce core.Nonce) int {
	tx := drainVault{
		owner:     principal,
		vault:     vault,
		recipient: to,
		amount:    uint64(amount),
	}
	return t.accounts[principal].baseGas() + t.accounts[vault].spendGas() + len(tx.gen(t).Raw)*int(t.VM.cfg.StorageCostFactor)
}

func encodeFields(tb testing.TB, fields ...scale.Encodable) types.RawTx {
	tb.Helper()

	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		require.NoError(tb, err)
	}
	return types.NewRawTx(buf.Bytes())
}

type testTx interface {
	gen(*tester) types.RawTx
}

type selfSpawnTx struct {
	principal int
}

func (tx *selfSpawnTx) gen(t *tester) types.RawTx {
	return t.selfSpawn(tx.principal)
}

type selfSpawnTxWithOpts struct {
	principal int
	opts      []sdk.Opt
}

func (tx *selfSpawnTxWithOpts) gen(t *tester) types.RawTx {
	return t.selfSpawn(tx.principal, tx.opts...)
}

type spawnTx struct {
	principal, target int
}

func (tx *spawnTx) gen(t *tester) types.RawTx {
	return t.spawn(tx.principal, tx.target)
}

type spawnTxWithOpts struct {
	principal, target int
	opts              []sdk.Opt
}

func (tx *spawnTxWithOpts) gen(t *tester) types.RawTx {
	return t.spawn(tx.principal, tx.target, tx.opts...)
}

type spendTx struct {
	from, to int
	amount   uint64
}

func (tx *spendTx) gen(t *tester) types.RawTx {
	return t.spend(tx.from, tx.to, tx.amount)
}

func (tx spendTx) withNonce(nonce core.Nonce) *spendNonce {
	return &spendNonce{spendTx: tx, nonce: nonce}
}

type spendTxWithOpts struct {
	from, to int
	amount   uint64
	opts     []sdk.Opt
}

func (tx *spendTxWithOpts) gen(t *tester) types.RawTx {
	return t.spend(tx.from, tx.to, tx.amount, tx.opts...)
}

type drainVault struct {
	owner, vault, recipient int
	amount                  uint64
}

func (tx *drainVault) gen(t *tester) types.RawTx {
	account := t.accounts[tx.owner]
	nonce := t.nextNonce(tx.owner)
	return types.NewRawTx(account.foreignCall(
		t.accounts[tx.vault].getAddress(),
		core.MethodSpend,
		&vault.SpendArguments{
			Destination: t.accounts[tx.recipient].getAddress(),
			Amount:      tx.amount,
		},
		nonce,
	))
}

type drainVaultWithOpts struct {
	owner, vault, recipient int
	amount                  uint64
	opts                    []sdk.Opt
}

func (tx *drainVaultWithOpts) gen(t *tester) types.RawTx {
	account := t.accounts[tx.owner]
	nonce := t.nextNonce(tx.owner)
	return types.NewRawTx(account.foreignCall(
		t.accounts[tx.vault].getAddress(),
		core.MethodSpend,
		&vault.SpendArguments{
			Destination: t.accounts[tx.recipient].getAddress(),
			Amount:      tx.amount,
		},
		nonce,
		tx.opts...,
	))
}

type corruptSig struct {
	testTx
}

func (cs corruptSig) gen(t *tester) types.RawTx {
	tx := cs.testTx.gen(t)
	last := tx.Raw[len(tx.Raw)-1]
	if last == 255 {
		last--
	} else {
		last++
	}
	tx.Raw[len(tx.Raw)-1] = last
	return tx
}

type spendNonce struct {
	spendTx
	nonce core.Nonce
}

func (tx *spendNonce) gen(t *tester) types.RawTx {
	return t.spendWithNonce(tx.from, tx.to, tx.amount, tx.nonce)
}

type change interface {
	verify(tb testing.TB, prev, current *core.Account)
}

type same struct{}

func (ch same) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()
	require.Equal(tb, prev, current)
}

type spawned struct {
	template core.Address
	change
}

func (ch spawned) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()

	require.Nil(tb, prev.TemplateAddress)
	require.Nil(tb, prev.State)

	require.NotNil(tb, current.TemplateAddress, "account should be spawned")
	require.Equal(tb, ch.template, *current.TemplateAddress)
	require.NotNil(tb, current.State)

	prev.TemplateAddress = current.TemplateAddress
	prev.State = current.State
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type earned struct {
	amount int
	change
}

func (ch earned) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()
	require.Equal(tb, ch.amount, int(current.Balance-prev.Balance))

	prev.Balance = current.Balance
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type spent struct {
	amount int
	change change
}

func (ch spent) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()
	require.Equal(tb, ch.amount, int(prev.Balance-current.Balance))

	prev.Balance = current.Balance
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type nonce struct {
	increased int
	change    change
}

func (ch nonce) verify(tb testing.TB, prev, current *core.Account) {
	require.Equal(tb, ch.increased, int(current.NextNonce-prev.NextNonce))
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type templateTestCase struct {
	desc   string
	layers []layertc
}

type layertc struct {
	txs      []testTx
	rewards  []reward
	expected map[int]change
	gasLimit uint64

	ineffective []int            // list with references to ineffective txs
	failed      map[int]error    // map with references to failed transaction, with specified error
	headers     map[int]struct{} // is vm expected to return the header
}

func singleWalletTestCases(defaultGasPrice int, template core.Address, ref *tester) []templateTestCase {
	return []templateTestCase{
		{
			desc: "Sanity",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
					expected: map[int]change{
						0: spawned{template: template},
						1: same{},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 100},
					},
					expected: map[int]change{
						0:  spent{amount: 100 + defaultGasPrice*ref.estimateSpendGas(0, 10, 100, 1)},
						1:  same{},
						10: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "wrong id for self-spawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTxWithOpts{0, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0: same{},
					},
				},
			},
		},
		{
			desc: "SpawnSpend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 10, 100},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{amount: 100 +
								defaultGasPrice*
									(ref.estimateSelfSpawnGas(0)+
										ref.estimateSpendGas(0, 10, 100, 1))},
						},
						10: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "wrong id for spend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTxWithOpts{0, 10, 100, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{1},
					expected: map[int]change{
						0: spawned{
							template: template,
							change:   spent{amount: defaultGasPrice * ref.estimateSelfSpawnGas(0)},
						},
						10: same{},
					},
				},
			},
		},
		{
			desc: "MultipleSpends",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 100},
						&spendTx{0, 11, 100},
						&spendTx{0, 12, 100},
					},
					expected: map[int]change{
						0: spent{amount: 100*3 + defaultGasPrice*
							(ref.estimateSpendGas(0, 10, 100, 1)+
								ref.estimateSpendGas(0, 11, 100, 2)+
								ref.estimateSpendGas(0, 12, 100, 3))},
						10: earned{amount: 100},
						11: earned{amount: 100},
						12: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "SpendReceived",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 10_000},
						&selfSpawnTx{10},
						&spendTx{10, 11, 100},
					},
					expected: map[int]change{
						0: spent{amount: 10000 + defaultGasPrice*
							ref.estimateSpendGas(0, 10, 10_000, 1)},
						10: spawned{
							template: template,
							change: earned{amount: 10000 - 100 - defaultGasPrice*(ref.estimateSelfSpawnGas(10)+
								ref.estimateSpendGas(10, 11, 100, 1))},
						},
						11: earned{amount: 100},
					},
				},
				{
					txs: []testTx{
						&spendTx{10, 11, 100},
						&spendTx{10, 12, 100},
					},
					expected: map[int]change{
						10: spent{amount: 2*100 + defaultGasPrice*
							(ref.estimateSpendGas(10, 11, 100, 2)+
								ref.estimateSpendGas(10, 12, 100, 3))},
						11: earned{amount: 100},
						12: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "StateChangedTransfer",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&selfSpawnTx{1},
					},
				},
				{
					txs: []testTx{
						&spendTx{1, 0, 1000},
						&spendTx{0, 10, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 10, 1000, 1),
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*ref.estimateSpendGas(1, 0, 1000, 1)},
						10: earned{amount: 1000},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 1000},
						&spendTx{1, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 10, 1000, 1),
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*ref.estimateSpendGas(1, 0, 1000, 1)},
						10: earned{amount: 1000},
					},
				},
			},
		},
		{
			desc: "SendToIself",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 0, 1000, 1),
							change: nonce{increased: 1},
						},
					},
				},
			},
		},
		{
			desc: "SpendNoSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&spendTx{0, 10, 1},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0:  same{},
						10: same{},
					},
				},
			},
		},
		{
			desc: "NoFundsForSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{11},
					},
					ineffective: []int{0},
					expected: map[int]change{
						11: same{},
					},
				},
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(11)) - 1},
						&selfSpawnTx{11},
					},
					failed: map[int]error{2: core.ErrNoBalance},
					expected: map[int]change{
						// incresed by two because previous was ineffective
						// but internal nonce in tester was incremented
						11: nonce{increased: 2},
					},
				},
			},
		},
		{
			desc: "NoFundsForSpend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(0) * defaultGasPrice)},
						&selfSpawnTx{11},
						&spendTx{11, 12, 1},
					},
					ineffective: []int{3},
					expected: map[int]change{
						11: spawned{template: template, change: nonce{increased: 1}},
						12: same{},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, uint64((ref.estimateSpendGas(11, 12, 1, 1) - 1) * defaultGasPrice)},
						// send enough funds to cover spawn, but no spend
						&spendTx{11, 12, 1},
					},
					failed: map[int]error{1: core.ErrNoBalance},
					expected: map[int]change{
						12: same{},
					},
				},
			},
		},
		{
			desc: "BlockGasLimit",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 10, 100},
						&spendTx{0, 11, 100},
						&spendTx{0, 12, 100},
					},
					gasLimit: uint64(ref.estimateSelfSpawnGas(0) +
						ref.estimateSpendGas(0, 10, 100, 1)),
					ineffective: []int{2, 3},
					expected: map[int]change{
						0:  spent{amount: 100 + ref.estimateSelfSpawnGas(0) + ref.estimateSpendGas(0, 10, 100, 1)},
						10: earned{amount: 100},
						11: same{},
						12: same{},
					},
				},
			},
		},
		{
			desc: "BlockGasLimitIsNotConsumedByInefective",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 10, uint64(ref.estimateSelfSpawnGas(1)) - 1},
						&selfSpawnTx{10},
						&spendTx{0, 11, 100},
					},
					gasLimit: uint64(ref.estimateSelfSpawnGas(0) +
						ref.estimateSpendGas(0, 10, 100, 1) +
						ref.estimateSelfSpawnGas(1)),
					failed:      map[int]error{2: core.ErrNoBalance},
					ineffective: []int{3},
					expected: map[int]change{
						0: spent{amount: ref.estimateSelfSpawnGas(1) - 1 +
							ref.estimateSelfSpawnGas(0) +
							ref.estimateSpendGas(0, 10, 100, 1)},
						10: nonce{increased: 1},
						11: same{},
					},
				},
			},
		},
		{
			desc: "BadNonceOrder",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						spendTx{0, 11, 100}.withNonce(2),
						spendTx{0, 10, 100}.withNonce(1),
					},
					ineffective: []int{2},
					headers: map[int]struct{}{
						2: {},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change:   spent{amount: 100 + defaultGasPrice*(ref.estimateSelfSpawnGas(0)+ref.estimateSpendGas(0, 11, 100, 2))},
						},
						10: same{},
						11: earned{amount: 100},
					},
				},
				{
					txs: []testTx{
						spendTx{0, 10, 100}.withNonce(3),
						spendTx{0, 12, 100}.withNonce(6),
					},
					expected: map[int]change{
						0: spent{amount: 2*100 + defaultGasPrice*(ref.estimateSpendGas(0, 10, 100, 3)+
							ref.estimateSpendGas(0, 10, 100, 6))},
						10: earned{amount: 100},
						12: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "SpendRewards",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: int(rewards.TotalSubsidyAtLayer(0)) + ref.estimateSelfSpawnGas(0)},
					},
				},
				{
					txs: []testTx{
						&selfSpawnTx{10},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: spawned{template: template},
					},
				},
			},
		},
		{
			desc: "DistributeRewards",
			layers: []layertc{
				{
					rewards: []reward{{address: 10, share: 0.5}, {address: 11, share: 0.5}},
					expected: map[int]change{
						10: earned{amount: int(rewards.TotalSubsidyAtLayer(0)) / 2},
						11: earned{amount: int(rewards.TotalSubsidyAtLayer(0)) / 2},
					},
				},
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
					rewards: []reward{{address: 10, share: 0.5}, {address: 11, share: 0.5}},
					expected: map[int]change{
						10: earned{amount: (int(rewards.TotalSubsidyAtLayer(1)) + ref.estimateSelfSpawnGas(10)) / 2},
						11: earned{amount: (int(rewards.TotalSubsidyAtLayer(1)) + ref.estimateSelfSpawnGas(11)) / 2},
					},
				},
			},
		},
		{
			desc: "SkippedTransactionsNotRewarded",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						spendTx{0, 10, 100}.withNonce(5),
					},
				},
				{
					txs: []testTx{
						spendTx{0, 10, 100}.withNonce(2),
						spendTx{0, 11, 100}.withNonce(3),
					},
					ineffective: []int{0, 1},
					headers:     map[int]struct{}{0: {}, 1: {}},
					rewards:     []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: int(rewards.TotalSubsidyAtLayer(1))},
					},
				},
			},
		},
		{
			desc: "FailVerify",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						corruptSig{&spendTx{0, 10, 100}},
					},
					ineffective: []int{0},
				},
			},
		},
		{
			desc: "RetrySpend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(11) + ref.estimateSpendGas(11, 12, 1_000, 1))},
						&selfSpawnTx{11},
						&spendTx{11, 12, 1_000},
					},
					failed: map[int]error{3: core.ErrNoBalance},
					expected: map[int]change{
						11: spawned{template: template, change: nonce{increased: 2}},
						12: same{},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, uint64(ref.estimateSpendGas(11, 12, 1_000, 2)) + 1_000},
						&spendTx{11, 12, 1_000},
					},
					expected: map[int]change{
						0:  spent{amount: ref.estimateSpendGas(11, 12, 1_000, 2)*2 + 1_000, change: nonce{increased: 1}},
						11: nonce{increased: 1},
						12: earned{amount: 1_000},
					},
				},
			},
		},
		{
			desc: "RetrySelfSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(11)) - 1},
						&selfSpawnTx{11},
					},
					failed: map[int]error{2: core.ErrNoBalance},
					expected: map[int]change{
						0: spent{amount: ref.estimateSelfSpawnGas(11) - 1 +
							ref.estimateSelfSpawnGas(0) +
							ref.estimateSpendGas(0, 11, ref.estimateSelfSpawnGas(11)-1, 1)},
						11: nonce{increased: 1},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(11))},
						&selfSpawnTx{11},
					},
					expected: map[int]change{
						0: spent{amount: ref.estimateSelfSpawnGas(11) +
							ref.estimateSpendGas(0, 11, ref.estimateSelfSpawnGas(11), 2)},
						11: spawned{template: template, change: nonce{increased: 1}},
					},
				},
			},
		},
		{
			desc: "SelfSpawnFailed",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&selfSpawnTx{0},
					},
					failed: map[int]error{1: core.ErrSpawned},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: nonce{
								increased: 2,
								change:    spent{amount: 2 * ref.estimateSelfSpawnGas(0)},
							},
						},
					},
				},
			},
		},
		{
			desc: "FailedFeesAndGas",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						// gas will be higher than fixed, but less than max gas
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(11)) - 1},
						// it will cause this transaction to be failed
						&selfSpawnTx{11},
					},
					gasLimit: uint64(ref.estimateSelfSpawnGas(0) +
						ref.estimateSpendGas(0, 11, ref.estimateSelfSpawnGas(11)-1, 1) +
						ref.estimateSelfSpawnGas(11)),
					failed:  map[int]error{2: core.ErrNoBalance},
					rewards: []reward{{address: 20, share: 1}},
					expected: map[int]change{
						0: spent{amount: ref.estimateSelfSpawnGas(0) +
							ref.estimateSpendGas(0, 11, ref.estimateSelfSpawnGas(11)-1, 1) +
							ref.estimateSelfSpawnGas(11) - 1},
						11: nonce{increased: 1},
						// fees from every transaction (including failed) + testBaseReward
						20: earned{amount: ref.estimateSelfSpawnGas(0) +
							ref.estimateSpendGas(0, 11, ref.estimateSelfSpawnGas(11)-1, 1) +
							ref.estimateSelfSpawnGas(11) - 1 +
							int(rewards.TotalSubsidyAtLayer(0))},
					},
				},
			},
		},
		{
			desc: "SpawnAnother",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 11},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{
								amount: ref.estimateSelfSpawnGas(0) + ref.estimateSpawnGas(0),
								change: nonce{increased: 2},
							},
						},
						11: spawned{template: template},
					},
				},
			},
		},
		{
			desc: "wrong id in spawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTxWithOpts{0, 11, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{1},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{
								amount: ref.estimateSelfSpawnGas(0),
								change: nonce{increased: 1},
							},
						},
						11: same{},
					},
				},
			},
		},
		{
			desc: "SpendFromSpawned",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 11},
						&spendTx{0, 11, 200 + uint64(ref.estimateSpendGas(11, 12, 200, 0))},
					},
					expected: map[int]change{
						11: spawned{template: template},
					},
				},
				{
					txs: []testTx{
						&spendTx{11, 12, 200},
					},
					expected: map[int]change{
						11: spent{amount: 200 +
							ref.estimateSpendGas(11, 12, 200, 0)},
						12: earned{amount: 200},
					},
				},
			},
		},
		{
			desc: "FailedSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSelfSpawnGas(11) + ref.estimateSpawnGas(11) - 1)},
						&selfSpawnTx{11},
						&spawnTx{11, 12},
					},
					expected: map[int]change{
						0:  spawned{template: template, change: nonce{increased: 2}},
						11: spawned{template: template, change: nonce{increased: 2}},
						12: same{},
					},
					failed: map[int]error{
						3: core.ErrNoBalance,
					},
				},
				{
					txs: []testTx{
						&spawnTx{0, 12},
					},
					expected: map[int]change{
						12: spawned{template: template},
					},
				},
			},
		},
		{
			desc: "NotSpawned",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnTx{0, 11},
					},
					expected: map[int]change{
						0:  same{},
						11: same{},
					},
					ineffective: []int{0},
				},
			},
		},
		{
			desc: "inefective zero gas price",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTxWithOpts{0, []sdk.Opt{sdk.WithGasPrice(0)}},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0: same{},
					},
				},
			},
		},
	}
}

func runTestCases(t *testing.T, tcs []templateTestCase, genTester func(t *testing.T) *tester) {
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			tt := genTester(t)
			next := types.GetEffectiveGenesis()
			for i, layer := range tc.layers {
				var txs []types.RawTx
				for _, gen := range layer.txs {
					txs = append(txs, gen.gen(tt))
				}
				lid := next
				next = next.Add(1)
				ctx := testContext(lid)
				if layer.gasLimit > 0 {
					tt = tt.withGasLimit(layer.gasLimit)
				}
				ineffective, results, err := tt.Apply(ctx, notVerified(txs...), tt.rewards(layer.rewards...))
				require.NoError(tt, err)
				if layer.ineffective == nil {
					require.Empty(tt, ineffective)
				} else {
					require.Len(tt, ineffective, len(layer.ineffective))
					for i, pos := range layer.ineffective {
						require.Equal(t, txs[pos].ID, ineffective[i].ID)
						_, exist := layer.headers[pos]
						if exist {
							require.NotNil(t, ineffective[i].TxHeader)
						} else {
							require.Nil(t, ineffective[i].TxHeader)
						}
					}
				}
				for i, rst := range results {
					expected, exists := layer.failed[i]
					if !exists {
						require.Equal(t, types.TransactionSuccess.String(), rst.Status.String(), "layer=%s ith=%d", lid, i)
					} else {
						require.Equal(t, types.TransactionFailure.String(), rst.Status.String(), "layer=%s ith=%d", lid, i)
						require.Equal(t, expected.Error(), rst.Message)
					}
				}
				for account, changes := range layer.expected {
					prev, err := accounts.Get(tt.db, tt.accounts[account].getAddress(), lid.Sub(1))
					require.NoError(tt, err)
					current, err := accounts.Get(tt.db, tt.accounts[account].getAddress(), lid)
					require.NoError(tt, err)
					tt.Logf("verifying account index=%d in layer index=%d", account, i)
					changes.verify(tt, &prev, &current)
				}
			}
		})
	}
}

func testWallet(t *testing.T, defaultGasPrice int, template core.Address, genTester func(t *testing.T) *tester) {
	runTestCases(t,
		singleWalletTestCases(defaultGasPrice, template, genTester(t)),
		genTester,
	)
}

func TestWallets(t *testing.T) {
	const (
		funded  = 10  // number of funded accounts, included in genesis
		total   = 100 // total number of accounts
		balance = 1_000_000_000

		defaultGasPrice = 1
	)
	t.Run("SingleSig", func(t *testing.T) {
		testWallet(t, defaultGasPrice, wallet.TemplateAddress, func(t *testing.T) *tester {
			return newTester(t).
				addSingleSig(funded).
				applyGenesisWithBalance(balance).
				addSingleSig(total - funded)
		})
	})
	t.Run("MultiSig13", func(t *testing.T) {
		const n = 3
		testWallet(t, defaultGasPrice, multisig.TemplateAddress1, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(funded, 1, n, multisig.TemplateAddress1).
				applyGenesisWithBalance(balance).
				addMultisig(total-funded, 1, n, multisig.TemplateAddress1)
		})
	})
	t.Run("MultiSig25", func(t *testing.T) {
		const n = 5
		testWallet(t, defaultGasPrice, multisig.TemplateAddress2, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(funded, 2, n, multisig.TemplateAddress2).
				applyGenesisWithBalance(balance).
				addMultisig(total-funded, 2, n, multisig.TemplateAddress2)
		})
	})
	t.Run("MultiSig310", func(t *testing.T) {
		const n = 10
		testWallet(t, defaultGasPrice, multisig.TemplateAddress3, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(funded, 3, n, multisig.TemplateAddress3).
				applyGenesisWithBalance(balance).
				addMultisig(total-funded, 3, n, multisig.TemplateAddress3)
		})
	})
}

func TestRandomTransfers(t *testing.T) {
	tt := newTester(t).withSeed(101).
		addSingleSig(10).
		addMultisig(10, 1, 3, multisig.TemplateAddress1).
		addMultisig(10, 2, 5, multisig.TemplateAddress2).
		addMultisig(10, 3, 10, multisig.TemplateAddress3).
		applyGenesis()

	skipped, _, err := tt.Apply(testContext(types.GetEffectiveGenesis()),
		notVerified(tt.spawnAll()...), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)
	for i := 1; i < 1000; i++ {
		lid := types.GetEffectiveGenesis().Add(uint32(i))
		skipped, _, err := tt.Apply(testContext(lid),
			notVerified(tt.randSpendN(20, 10)...), nil)
		require.NoError(tt, err)
		require.Empty(tt, skipped)
	}
}

func testValidation(t *testing.T, tt *tester, template core.Address) {
	skipped, _, err := tt.Apply(testContext(types.GetEffectiveGenesis()),
		notVerified(tt.selfSpawn(0)), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	firstAddress := tt.accounts[0].getAddress()
	zero := scale.U8(0)
	one := scale.U8(1)
	two := scale.U8(2)

	for _, tc := range []struct {
		desc     string
		tx       types.RawTx
		header   *core.Header
		err      error
		verified bool
	}{
		{
			desc: "Spawn",
			tx:   tt.selfSpawn(1),
			header: &core.Header{
				Principal:       tt.accounts[1].getAddress(),
				TxType:          core.SelfSpawn,
				TemplateAddress: template,
				GasPrice:        1,
				MaxGas:          uint64(tt.estimateSelfSpawnGas(1)),
			},
			verified: true,
		},
		{
			desc: "spawn genesis id mismatch",
			tx:   tt.selfSpawn(1, sdk.WithGenesisID(types.Hash20{1})),
		},
		{
			desc: "Spend",
			tx:   tt.spend(0, 1, 100),
			header: &core.Header{
				Principal:       tt.accounts[0].getAddress(),
				TxType:          core.LocalMethodCall,
				Method:          core.MethodSpend,
				TemplateAddress: template,
				GasPrice:        1,
				Nonce:           1,
				MaxSpend:        100,
				MaxGas:          uint64(tt.estimateSpendGas(0, 1, 100, 1)),
			},
			verified: true,
		},
		{
			desc: "spawn genesis id mismatch",
			tx:   tt.spend(0, 1, 100, sdk.WithGenesisID(types.Hash20{1})),
		},
		{
			desc: "WrongVersion",
			tx:   encodeFields(tt, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "InvalidPrincipal",
			tx:   encodeFields(tt, &one, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "InvalidTemplate",
			tx:   encodeFields(tt, &zero, &firstAddress, &zero, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "UnknownTemplate",
			tx:   encodeFields(tt, &zero, &firstAddress, &zero, &firstAddress),
			err:  core.ErrMalformed,
		},
		{
			desc: "UnknownMethod",
			tx:   encodeFields(tt, &zero, &firstAddress, &two),
			err:  core.ErrMalformed,
		},
		{
			desc: "NotSpawned",
			tx:   tt.spend(1, 1, 100),
			err:  core.ErrNotSpawned,
		},
		{
			desc: "SpawnNotSpawned",
			tx:   tt.spawn(1, 0),
			err:  core.ErrNotSpawned,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := tt.Validation(tc.tx)
			header, err := req.Parse()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.Equal(t, tc.verified, req.Verify())
				if tc.verified {
					require.Equal(t, tc.header, header)
				}
			}
		})
	}
}

func testSpawnOther(t *testing.T, genTester func(t *testing.T) *tester) {
	ref := genTester(t)
	genTestCase := func(i, j int) templateTestCase {
		return templateTestCase{
			desc: fmt.Sprintf("%d spawns %d", i, j),
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{i},
						&spawnTx{i, j},
					},
					expected: map[int]change{
						i: spawned{template: ref.accounts[i].getTemplate()},
						j: spawned{template: ref.accounts[j].getTemplate()},
					},
				},
				{
					txs: []testTx{
						&spendTx{j, i, 1000},
					},
					expected: map[int]change{
						i: earned{amount: 1000},
						j: spent{amount: 1000 + ref.estimateSpendGas(j, i, 1000, 0)},
					},
				},
			},
		}
	}
	runTestCases(t, []templateTestCase{genTestCase(0, 1), genTestCase(1, 0)}, genTester)
}

func TestSpawnOtherTemplate(t *testing.T) {
	t.Run("SingleSig/Multisig13", func(t *testing.T) {
		testSpawnOther(t, func(t *testing.T) *tester {
			return newTester(t).
				addSingleSig(1).
				addMultisig(1, 1, 3, multisig.TemplateAddress1).
				applyGenesis()
		})
	})
	t.Run("SingleSig/Multisig25", func(t *testing.T) {
		testSpawnOther(t, func(t *testing.T) *tester {
			return newTester(t).
				addSingleSig(1).
				addMultisig(1, 2, 5, multisig.TemplateAddress2).
				applyGenesis()
		})
	})
	t.Run("SingleSig/Multisig37", func(t *testing.T) {
		testSpawnOther(t, func(t *testing.T) *tester {
			return newTester(t).
				addSingleSig(1).
				addMultisig(1, 3, 7, multisig.TemplateAddress3).
				applyGenesis()
		})
	})
	t.Run("MultiSig13/Multisig25", func(t *testing.T) {
		testSpawnOther(t, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(1, 1, 3, multisig.TemplateAddress1).
				addMultisig(1, 2, 5, multisig.TemplateAddress2).
				applyGenesis()
		})
	})
	t.Run("MultiSig13/Multisig37", func(t *testing.T) {
		testSpawnOther(t, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(1, 1, 3, multisig.TemplateAddress1).
				addMultisig(1, 3, 7, multisig.TemplateAddress3).
				applyGenesis()
		})
	})
	t.Run("MultiSig25/Multisig37", func(t *testing.T) {
		testSpawnOther(t, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(1, 2, 5, multisig.TemplateAddress2).
				addMultisig(1, 3, 7, multisig.TemplateAddress3).
				applyGenesis()
		})
	})
}

func TestVestingWithVault(t *testing.T) {
	const (
		initial = 1_000
		total   = 11_000
		start   = 2
		end     = 4

		multisigAccounts = 20 // number of funded vesting accounts
		vaultAccounts    = 10 // number of funded vault accounts

		// in the test below
		// vesting with funds:  [0 : 20)
		// vesting with vaults: [0 : 10)
		// vault accounts:      [20 : 30)
	)
	var (
		multisigTemplate = multisig.TemplateAddress1
		vaultTemplate    = vault.TemplateAddress
	)

	genTester := func(t *testing.T) *tester {
		return newTester(t).
			addMultisig(multisigAccounts, 1, 2, multisigTemplate).
			addVault(10, total, initial, types.GetEffectiveGenesis().Add(start-1), types.GetEffectiveGenesis().Add(end-1)).
			applyGenesis()
	}
	ref := genTester(t)
	tcs := []templateTestCase{
		{
			desc: "sanity",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 20},
					},
					expected: map[int]change{
						0:  spawned{template: multisigTemplate},
						20: spawned{template: vaultTemplate},
					},
				},
				{
					txs: []testTx{
						&drainVault{0, 20, 11, 500},
					},
					expected: map[int]change{
						0:  spent{amount: ref.estimateDrainGas(0, 20, 11, 500, 2)},
						20: spent{amount: 500},
						11: earned{amount: 500},
					},
				},
			},
		},
		{
			desc: "owner is not overwritten by principal",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&selfSpawnTx{1},
						// 20 is owned by 0, so drain vault wont work for 1
						&spawnTx{1, 20},
					},
					expected: map[int]change{
						0:  spawned{template: multisigTemplate},
						1:  spawned{template: multisigTemplate},
						20: spawned{template: vaultTemplate},
					},
				},
				{
					txs: []testTx{
						&drainVault{
							owner: 1, vault: 20, recipient: 11,
							amount: 100,
						},
					},
					failed: map[int]error{
						0: core.ErrAuth,
					},
					expected: map[int]change{
						0: same{},
						1: spent{
							amount: ref.estimateDrainGas(1, 20, 11, 100, 1),
							change: nonce{increased: 1},
						},
						11: same{},
						20: same{},
					},
				},
			},
		},
		{
			desc: "dont transfer more than available",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 20},
						&drainVault{0, 20, 11, 500},
					},
					failed: map[int]error{
						2: vault.ErrAmountNotAvailable,
					},
					expected: map[int]change{
						0:  spawned{template: multisigTemplate},
						20: spawned{template: vaultTemplate},
					},
				},
				{
					txs: []testTx{
						&drainVault{
							owner: 0, vault: 20, recipient: 11,
							amount: 1001,
						},
						&drainVault{
							owner: 0, vault: 20, recipient: 11,
							amount: 1000,
						},
						&drainVault{
							owner: 0, vault: 20, recipient: 11,
							amount: 1000,
						},
					},
					failed: map[int]error{
						0: vault.ErrAmountNotAvailable,
						2: vault.ErrAmountNotAvailable,
					},
					expected: map[int]change{
						0: spent{
							amount: ref.estimateDrainGas(0, 20, 11, 1001, 3) +
								ref.estimateDrainGas(0, 20, 11, 1000, 4) +
								ref.estimateDrainGas(0, 20, 11, 1000, 5),
						},
						11: earned{amount: 1000},
						20: spent{amount: 1000},
					},
				},
				{
					txs: []testTx{
						&drainVault{
							owner: 0, vault: 20, recipient: 10,
							amount: 10000,
						},
						&drainVault{
							owner: 0, vault: 20, recipient: 10,
							amount: 5000,
						},
					},
					failed: map[int]error{
						0: vault.ErrAmountNotAvailable,
					},
					expected: map[int]change{
						0: spent{amount: ref.estimateDrainGas(0, 20, 10, 10000, 5) +
							ref.estimateDrainGas(0, 20, 10, 5000, 6)},
						10: earned{amount: 5000},
						20: spent{amount: 5000},
					},
				},
				{}, // note the layer without transactions
				{
					txs: []testTx{
						&drainVault{
							owner: 0, vault: 20, recipient: 9,
							amount: 5001,
						},
						&drainVault{
							owner: 0, vault: 20, recipient: 9,
							amount: 5000,
						},
					},
					failed: map[int]error{
						0: vault.ErrAmountNotAvailable,
					},
					expected: map[int]change{
						0: spent{amount: ref.estimateDrainGas(0, 20, 9, 5001, 7) +
							ref.estimateDrainGas(0, 20, 9, 5000, 8)},
						9:  earned{amount: 5000},
						20: spent{amount: 5000},
					},
				},
			},
		},
		{
			desc: "wrong id for drain vault",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 20},
					},
					expected: map[int]change{
						0:  spawned{template: multisigTemplate},
						20: spawned{template: vaultTemplate},
					},
				},
				{
					txs: []testTx{
						&drainVaultWithOpts{0, 20, 11, 500, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0:  same{},
						20: same{},
						11: same{},
					},
				},
			},
		},
	}
	runTestCases(t, tcs, genTester)
}

func TestValidation(t *testing.T) {
	t.Run("SingleSig", func(t *testing.T) {
		tt := newTester(t).
			addSingleSig(1).
			applyGenesis().
			addSingleSig(1)
		testValidation(t, tt, wallet.TemplateAddress)
	})
	t.Run("MultiSig13", func(t *testing.T) {
		tt := newTester(t).
			addMultisig(1, 1, 3, multisig.TemplateAddress1).
			applyGenesis().
			addMultisig(1, 1, 3, multisig.TemplateAddress1)
		testValidation(t, tt, multisig.TemplateAddress1)
	})
	t.Run("MultiSig25", func(t *testing.T) {
		tt := newTester(t).
			addMultisig(1, 2, 5, multisig.TemplateAddress2).
			applyGenesis().
			addMultisig(1, 2, 5, multisig.TemplateAddress2)
		testValidation(t, tt, multisig.TemplateAddress2)
	})
	t.Run("MultiSig310", func(t *testing.T) {
		tt := newTester(t).
			addMultisig(1, 3, 10, multisig.TemplateAddress3).
			applyGenesis().
			addMultisig(1, 3, 10, multisig.TemplateAddress3)
		testValidation(t, tt, multisig.TemplateAddress3)
	})
}

func TestVaultValidation(t *testing.T) {
	tt := newTester(t).
		addMultisig(1, 1, 2, multisig.TemplateAddress1).
		addVault(2, 100, 10, types.NewLayerID(1), types.NewLayerID(10)).
		applyGenesis()
	_, _, err := tt.Apply(ApplyContext{Layer: types.GetEffectiveGenesis()},
		notVerified(tt.selfSpawn(0), tt.spawn(0, 1)), nil)
	require.NoError(t, err)

	t.Run("self spawn", func(t *testing.T) {
		tx := sdk.Encode(
			&sdk.TxVersion,
			&sdk.SelfSpawn,
			&vault.TemplateAddress,
			sdk.LengthPrefixedStruct{Encodable: tt.accounts[2].spawnArgs()},
		)
		req := tt.Validation(types.NewRawTx(tx))
		header, err := req.Parse()
		require.NoError(t, err)
		require.NotNil(t, header)
		require.False(t, req.Verify())
	})
	t.Run("spawn", func(t *testing.T) {
		principal := tt.accounts[1].getAddress()
		tx := sdk.Encode(
			&sdk.TxVersion,
			&sdk.Spawn,
			&principal,
			&vault.TemplateAddress,
			sdk.LengthPrefixedStruct{Encodable: tt.accounts[2].spawnArgs()},
		)
		req := tt.Validation(types.NewRawTx(tx))
		header, err := req.Parse()
		require.NoError(t, err)
		require.NotNil(t, header)
		require.False(t, req.Verify())
	})
	t.Run("spend", func(t *testing.T) {
		principal := tt.accounts[1].getAddress()
		tx := sdk.Encode(
			&sdk.TxVersion,
			&sdk.LocalMethodCall,
			&principal,
			&sdk.MethodSpend,
			sdk.LengthPrefixedStruct{Encodable: &vault.SpendArguments{}},
		)
		req := tt.Validation(types.NewRawTx(tx))
		header, err := req.Parse()
		require.NoError(t, err)
		require.NotNil(t, header)
		require.False(t, req.Verify())
	})
}

func FuzzParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		tt := newTester(t).addSingleSig(1).applyGenesis()
		req := tt.Validation(types.NewRawTx(data))
		req.Parse()
	})
}

func BenchmarkValidation(b *testing.B) {
	tt := newTester(b).addSingleSig(2).applyGenesis()
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.NewLayerID(1)},
		notVerified(tt.selfSpawn(0)), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	bench := func(b *testing.B, raw types.RawTx) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			req := tt.Validation(raw)
			_, err := req.Parse()
			if err != nil {
				b.Fatal(err)
			}
			if !req.Verify() {
				b.Fatalf("expected Verify to return true")
			}
		}
	}

	b.Run("SpawnWallet", func(b *testing.B) {
		bench(b, tt.selfSpawn(1))
	})

	b.Run("SpendWallet", func(b *testing.B) {
		bench(b, tt.spend(0, 1, 10))
	})
}

func TestBeforeEffectiveGenesis(t *testing.T) {
	// sanity check that layers before effective genesis are not pushed to vm
	tt := newTester(t)
	_, _, err := tt.Apply(ApplyContext{Layer: types.GetEffectiveGenesis().Sub(1)}, nil, nil)
	require.ErrorIs(t, err, core.ErrInternal)
}

func TestStateHashFromUpdatedAccounts(t *testing.T) {
	tt := newTester(t).addSingleSig(10).applyGenesis()

	root, err := tt.GetStateRoot()
	require.NoError(t, err)
	require.Equal(t, types.Hash32{}, root)

	lid := types.GetEffectiveGenesis()
	skipped, _, err := tt.Apply(testContext(lid), notVerified(
		tt.selfSpawn(0),
		tt.selfSpawn(1),
		tt.spend(0, 2, 100),
		tt.spend(1, 4, 100),
	), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	expected := types.Hash32{}
	hasher := hash.New()
	encoder := scale.NewEncoder(hasher)
	for _, pos := range []int{0, 1, 2, 4} {
		account, err := accounts.Get(tt.db, tt.accounts[pos].getAddress(), lid)
		require.NoError(t, err)
		account.EncodeScale(encoder)
	}
	hasher.Sum(expected[:0])

	statehash, err := layers.GetStateHash(tt.db, lid)
	require.NoError(t, err)
	require.Equal(t, expected, statehash)

	root, err = tt.GetStateRoot()
	require.NoError(t, err)
	require.Equal(t, expected, root)
}

func BenchmarkWallet(b *testing.B) {
	b.Run("Accounts100k/Txs100k", func(b *testing.B) {
		benchmarkWallet(b, 100_000, 100_000)
	})
	b.Run("Accounts100k/Txs1kk", func(b *testing.B) {
		benchmarkWallet(b, 100_000, 1_000_000)
	})
}

func benchmarkWallet(b *testing.B, accounts, n int) {
	tt := newTester(b).persistent().
		addSingleSig(accounts).applyGenesis().withSeed(101)
	lid := types.NewLayerID(1)
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.NewLayerID(1)},
		notVerified(tt.spawnAll()...), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	var layers [][]types.Transaction
	for i := 0; i < b.N; i++ {
		raw := tt.randSpendN(n, 10)
		parsed := make([]types.Transaction, 0, len(raw))
		for _, tx := range raw {
			val := tt.Validation(tx)
			header, err := val.Parse()
			require.NoError(b, err)
			parsed = append(parsed, types.Transaction{
				RawTx:    tx,
				TxHeader: header,
			})
		}
		layers = append(layers, parsed)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for _, txs := range layers {
		lid = lid.Add(1)
		skipped, _, err := tt.Apply(testContext(lid), txs, nil)
		if err != nil {
			b.Fatal(err)
		}
		if skipped != nil {
			b.Fatalf("skipped transactions %v", skipped)
		}
	}
}

func notVerified(raw ...types.RawTx) []types.Transaction {
	var rst []types.Transaction
	for _, tx := range raw {
		rst = append(rst, types.Transaction{RawTx: tx})
	}
	return rst
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(2)
	os.Exit(m.Run())
}
