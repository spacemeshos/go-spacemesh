package tokenwallet

import (
	_ "embed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

//go:embed elf/wallet
var PROGRAM []byte

var TemplateAddress types.Address

func init() {
	TemplateAddress = core.TemplateAddress(PROGRAM)
}
