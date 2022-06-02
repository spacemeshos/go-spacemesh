package core

import (
	"bytes"
	"crypto/sha256"
	"errors"
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

	order   []Address
	changed map[Address]*Account
}

func (c *Context) Spawn() {
	var principal Address
	hasher := sha256.New()
	encoder := scale.NewEncoder(hasher)
	c.Account.Template.EncodeScale(encoder)
	c.Header.Nonce.EncodeScale(encoder)
	c.Args.EncodeScale(encoder)
	r1 := hasher.Sum(nil)
	copy(principal[:], r1)

	if principal != c.Principal {
		c.Fail(fmt.Errorf("only self spawn is supported: %x != %x", principal, c.Principal))
	}
}

func (c *Context) Transfer(to Address, amount uint64) {
	if amount > c.Account.Balance {
		c.Fail(errors.New("out of funds"))
	}
	c.Account.Balance -= amount
	if c.changed == nil {
		c.changed = map[Address]*Account{}
	}
	account, exist := c.changed[to]
	if !exist {
		loaded, err := c.Loader.Get(to)
		if err != nil {
			c.Fail(err)
		}
		c.changed[to] = &loaded
		account = &loaded
	}
	account.Balance += amount
}

func (c *Context) Consume(gas uint64) {
	amount := gas * c.Header.GasPrice
	if amount > c.Account.Balance {
		c.Fail(errors.New("out of funds"))
	}
	c.Account.Balance -= amount
}

func (c *Context) Apply(updater AccountUpdater) error {
	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	c.Template.EncodeScale(encoder)
	c.Account.Nonce = c.Header.Nonce.Counter
	c.Account.State = buf.Bytes()
	if err := updater.Update(c.Account); err != nil {
		return err
	}
	for _, address := range c.order {
		account := c.changed[address]
		if err := updater.Update(*account); err != nil {
			return err
		}
	}
	return nil
}

func (c *Context) Fail(err error) {
	panic(err)
}
