package main

import "fmt"

const word = 8

type charge struct {
	name string
	eth  int
	desc string
}

var (
	chargeTXDATA        = charge{"txdata (per 8)", 128, "charged for storing transaction data"}
	chargeSTORE         = charge{"store (per 8)", 5000, "charged for changing active state from zero to non-zero"}
	chargeACCOUNTACCESS = charge{"account access", 2500, "base cost for accessing account from storage"}
	chargeUPDATE        = charge{"update (per 8)", 725, "charged for updating active state"}
	chargeLOAD          = charge{"load (per 8)", 182, "charged for loading active state"}
	chargeEDVERIFY      = charge{"edverify (per sig)", 3000, "charged for ed25519 verification"}
	chargeSPAWN         = charge{"spawn", 30000, "charged for every spawn transaction"}
	chargeTX            = charge{"tx", 20000, "charged for every transaction"}
)

var charges = []charge{
	chargeTXDATA,
	chargeSTORE,
	chargeUPDATE,
	chargeLOAD,
	chargeACCOUNTACCESS,
	chargeEDVERIFY,
	chargeSPAWN,
	chargeTX,
}

type costFn func(charge) int

func eth(c charge) int {
	return c.eth
}

const (
	kindStore = iota
	kindUpdate
	kindLoad
	kindEdverify
	kindSpawn
	kindAccountsAccess
)

type tx struct {
	name string
	size int
	ops  []op
}

func perWord(size int) int {
	den := (size / word)
	rem := size % word
	if rem != 0 {
		return den + 1
	}
	return den
}

func (t tx) cost(f costFn) int {
	total := perWord(t.size) * f(chargeTXDATA)
	for _, o := range t.ops {
		total += o.cost(f)
	}
	return total + f(chargeTX)
}

type op struct {
	kind int
	size int
}

func (o op) cost(f costFn) int {
	switch o.kind {
	case kindStore:
		return perWord(o.size) * f(chargeSTORE)
	case kindUpdate:
		return perWord(o.size) * f(chargeUPDATE)
	case kindLoad:
		return perWord(o.size) * f(chargeLOAD)
	case kindEdverify:
		return o.size * f(chargeEDVERIFY)
	case kindSpawn:
		return f(chargeSPAWN)
	case kindAccountsAccess:
		return f(chargeACCOUNTACCESS)
	}
	panic("unknown")
}

func store(size int) op {
	return op{kindStore, size}
}

func update(size int) op {
	return op{kindUpdate, size}
}

func load(size int) op {
	return op{kindLoad, size}
}

func edverify(size int) op {
	return op{kindEdverify, size}
}

func spawn() op {
	return op{kindSpawn, 0}
}

func accountaccess() op {
	return op{kindAccountsAccess, 0}
}

func describe(name string, size int, ops ...op) tx {
	return tx{name: name, size: size, ops: ops}
}

const (
	sizeSpawn = 64
	sizeSpend = 56
)

func txs() []tx {
	txs := []tx{
		describe("singlesig/selfspawn", sizeSpawn+32+64, store(48), edverify(1), spawn()),
		describe("singlesig/spawn", sizeSpawn+32+64, accountaccess(), accountaccess(), load(48), store(48), update(16), edverify(1), spawn()),
		describe("singlesig/spend", sizeSpend+64, accountaccess(), accountaccess(), load(48), load(8), update(16), update(8), edverify(1)),
	}
	for n := 1; n <= 3; n++ {
		for k := 1; k <= 10; k++ {
			if n > k {
				continue
			}
			sigs := n * 64
			pubs := k * 32
			txs = append(txs,
				describe(fmt.Sprintf("multisig/%d/%d/selfspawn", n, k), sizeSpawn+sigs+pubs, store(16+pubs), edverify(n), spawn()),
				describe(fmt.Sprintf("multisig/%d/%d/spawn", n, k), sizeSpawn+sigs+pubs, load(16+pubs), accountaccess(), store(16+pubs), update(16), edverify(n), spawn()),
				describe(fmt.Sprintf("multisig/%d/%d/spend", n, k), sizeSpend+sigs, load(16+pubs), accountaccess(), accountaccess(), load(8), update(16), update(8), edverify(n)),
				describe(fmt.Sprintf("vesting/%d/%d/spawnvault", n, k), sizeSpawn+sigs+56, accountaccess(), load(sizeSpawn+sigs), store(80), update(16), edverify(n), spawn()),
				describe(fmt.Sprintf("vesting/%d/%d/drain", n, k), sizeSpend+24+sigs, accountaccess(), accountaccess(), load(16+pubs), load(80), edverify(n), update(16), update(16)),
			)
		}
	}
	return txs
}

const price = 8.3e-08

func main() {
	fmt.Println("| name | eth | description |")
	fmt.Println("| --- | --- | --- | ")
	for _, charge := range charges {
		fmt.Printf("| %s | %d | %s | \n", charge.name, charge.eth, charge.desc)
	}
	fmt.Println("------")
	fmt.Println("| name | gas (eth) | usd (eth) |")
	fmt.Println("| --- | --- | --- | ")
	for _, tx := range txs() {
		fmt.Printf("| %s | %d | %0.4f |\n", tx.name, tx.cost(eth), float64(tx.cost(eth))*price)
	}
}
