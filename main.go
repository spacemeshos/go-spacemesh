package main

import (
	"github.com/UnrulyOS/go-unruly/app"
	"github.com/UnrulyOS/go-unruly/log"
)

func main() {

	log.Info("App starting...")

	// test p2p protocols
	// node.TestP2pProtocols(nil)

	// run the app
	app.Main()

	// add any playground tests here....
}
