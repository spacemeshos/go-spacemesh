package core

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/spacemeshos/go-scale"
)

type Context struct {
	Loader   AccountLoader
	Handler  Handler
	Template Template

	Account   Account
	Principal Address
	Method    uint8

	Header Header
	Args   scale.Encodable

	consumed   uint64
	transfered uint64

	order   []Address
	changed map[Address]*Account
}

func (c *Context) Spawn() error {
	var principal Address
	hasher := sha256.New()
	encoder := scale.NewEncoder(hasher)
	c.Account.Template.EncodeScale(encoder)
	c.Header.Nonce.EncodeScale(encoder)
	c.Args.EncodeScale(encoder)
	r1 := hasher.Sum(nil)
	copy(principal[:], r1)
	if principal != c.Principal {
		return ErrSpawn
	}
	return nil
}

func (c *Context) Transfer(to Address, amount uint64) error {
	if amount > c.Account.Balance {
		return ErrNoBalance
	}
	if c.changed == nil {
		c.changed = map[Address]*Account{}
	}
	account, exist := c.changed[to]
	if !exist {
		loaded, err := c.Loader.Get(to)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrInternal, err.Error())
		}
		c.changed[to] = &loaded
		account = &loaded
	}
	c.transfered += amount
	if c.transfered > c.Header.MaxSpend {
		return ErrMaxSpend
	}

	c.Account.Balance -= amount
	account.Balance += amount
	return nil
}

func (c *Context) Consume(gas uint64) error {
	amount := gas * c.Header.GasPrice
	if amount > c.Account.Balance {
		return ErrNoBalance
	}
	c.consumed += amount
	if c.consumed > c.Header.MaxGas {
		return ErrMaxGas
	}
	c.Account.Balance -= amount
	return nil
}

func (c *Context) Apply(updater AccountUpdater) error {
	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	c.Template.EncodeScale(encoder)
	c.Account.Nonce = c.Header.Nonce.Counter
	c.Account.State = buf.Bytes()
	if err := updater.Update(c.Account); err != nil {
		return fmt.Errorf("%w: %s", ErrInternal, err.Error())
	}
	for _, address := range c.order {
		account := c.changed[address]
		if err := updater.Update(*account); err != nil {
			return fmt.Errorf("%w: %s", ErrInternal, err.Error())
		}
	}
	return nil
}
