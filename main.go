package main

import (
	node "github.com/UnrulyOS/go-unruly/node"
	app "github.com/UnrulyOS/go-unruly/app"
)



func main() {

	// test p2p protocols
	node.TestP2pProtocols()

	// run the app
	app.Main()

}
