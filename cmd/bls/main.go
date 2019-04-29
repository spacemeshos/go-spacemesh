package main

import (
	"fmt"
	"github.com/spacemeshos/go-bls"
)

func main() {
	m := []byte{1, 2, 3, 4}
	sk := bls.NewSecretKey()
	sig := sk.Sign(m)
	pub := sk.GetPublicKey()
	if !sig.Verify(pub, m) {
		fmt.Println("err")
		return
	}

	fmt.Println("ok")

}
