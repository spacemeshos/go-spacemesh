package main

import "fmt"

const word = 32

type charge struct {
	name string
	eth  int
	desc string
}

func (c charge) spacemesh() int {
	return c.eth / 256
}

var (
	chargeTXDATA   = charge{"txdata", 512, "charged for storing transaction data (per 32 bytes)"}
	chargeSTORE    = charge{"store", 20000, "charged for changing active state from zero to non-zero (per 32 bytes)"}
	chargeUPDATE   = charge{"update", 2900, "charged for updating active state (per 32 bytes)"}
	chargeLOAD     = charge{"load", 800, "charged for loading active state (per 32 bytes)"}
	chargeEDVERIFY = charge{"edverify", 3000, "charged for ed25519 verification (per signature)"}
	chargeSPAWN    = charge{"spawn", 32000, "charged for every spawn transaction"}
	chargeTX       = charge{"tx", 21000, "charged for every transaction"}
)

var charges = []charge{
	chargeTXDATA,
	chargeSTORE,
	chargeUPDATE,
	chargeLOAD,
	chargeEDVERIFY,
	chargeSPAWN,
	chargeTX,
}

type costFn func(charge) int

func eth(c charge) int {
	return c.eth
}

func spacemesh(c charge) int {
	return c.spacemesh()
}

const (
	kindStore = iota
	kindUpdate
	kindLoad
	kindEdverify
	kindSpawn
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

func describe(name string, size int, ops ...op) tx {
	return tx{name: name, size: size, ops: ops}
}

var txs = []tx{
	describe("singlesig/selfspawn", 150, store(48), edverify(1), spawn()),
	describe("singlesig/spend", 120, load(48), load(8), update(16), update(8), edverify(1)),
	describe("singlesig/spawn", 150, store(48), load(16), update(16), edverify(1), spawn()),
	describe("multisig/3/5/selfspawn", 410, store(176), edverify(3), spawn()),
	describe("multisig/3/5/spend", 250, load(176), load(16), update(16), update(8), edverify(3)),
	describe("multisig/3/10/selfspawn", 568, store(337), edverify(3), spawn()),
	describe("multisig/3/10/spend", 250, load(337), load(16), update(16), update(16), edverify(3)),
}

const price = 8.3e-08

func main() {
	fmt.Println("| name | eth | spacemesh | description |")
	fmt.Println("| - | - | - | - |")
	for _, charge := range charges {
		fmt.Printf("| %s | %d | %d | %s | \n", charge.name, charge.eth, charge.spacemesh(), charge.desc)
	}
	fmt.Println("------")
	fmt.Println("| name | gas (eth) | gas (spacemesh) | usd (eth) |")
	fmt.Println("| - | - | - | - | ")
	for _, tx := range txs {
		fmt.Printf("| %s | %d | %d | %0.4f |\n", tx.name, tx.cost(eth), tx.cost(spacemesh), float64(tx.cost(eth))*price)
	}
}
