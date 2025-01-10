package wallet

import (
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
}

var TemplateAddress core.Address
