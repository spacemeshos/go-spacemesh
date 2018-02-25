package net

import (
	"errors"
	"fmt"
	"net"

	"github.com/spacemeshos/go-spacemesh/crypto"
)

// CheckUserPort tries to listen on a port and check whether is usable or not
func CheckUserPort(port uint32) bool {
	address := fmt.Sprintf("0.0.0.0:%d", port)
	l, e := net.Listen("tcp", address)
	if e != nil {
		return true
	}
	l.Close()
	return false
}

// GetUnboundedPort returns a port that is for sure unbounded or an error.
func GetUnboundedPort() (int, error) {
	port := crypto.GetRandomUserPort()
	retryCount := 0
	for used := true; used && retryCount < 10; used, retryCount = CheckUserPort(port), retryCount+1 {
		port = crypto.GetRandomUserPort()
	}
	if retryCount >= 10 {
		return 0, errors.New("failed to establish network, probably no internet connection")
	}
	return int(port), nil
}
