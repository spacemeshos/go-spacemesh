package core

import (
	"bytes"
	"crypto/sha256"
	"errors"

	"github.com/spacemeshos/go-scale"
)

type Context struct {
	Handler  TemplateAPI
	Template Template

	State     *Account
	Principal Address
	Method    uint8

	templateAddress Address
	header          Header
	args            scale.Encodable
	spawned         struct {
		address scale.Address
		state   *Account
	}
	consumed uint64
	price    uint64
	funds    uint64
}

func (c *Context) Spawn() {
	hasher := sha256.New()
	encoder := scale.NewEncoder(hasher)
	c.templateAddress.EncodeScale(encoder)
	c.header.Nonce.EncodeScale(encoder)
	c.args.EncodeScale(encoder)
	hasher.Sum(c.spawned.address[:])

	if c.spawned.address != c.Principal {
		c.Fail(errors.New("only self spawn is supported"))
	}
	c.spawned.state = c.State

	buf := bytes.NewBuffer(nil)
	encoder = scale.NewEncoder(buf)
	c.Template.EncodeScale(encoder)

	c.spawned.state.Template = &c.templateAddress
	c.spawned.state.State = buf.Bytes()
}

func (c *Context) Transfer(to Address, value uint64) {}

func (c *Context) Consume(gas uint64) {
	c.consumed += gas
	if c.consumed*c.price+c.funds > c.State.Balance {
		c.Fail(errors.New("out of funds"))
	}
}

func (c *Context) Fail(err error) {
	//
	panic(err)
}
