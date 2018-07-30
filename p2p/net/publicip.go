package net

import (
	"flag"
	"fmt"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"io/ioutil"
	"net/http"
	"strings"
)

// GetPublicIPAddress returns this host public ip address.
// Method is implemented using the ipify.org service.
func GetPublicIPAddress() (string, error) {

	// We don't want to query services and determine public IP addresses on a test.
	if flag.Lookup("test.v") != nil {
		return "127.0.0.1", nil
	}

	// todo: make this more robust by adding additional fallback services so
	// if ipify.org can't bring down our whole network

	res, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}

	ip, err := ioutil.ReadAll(res.Body)
	return fmt.Sprintf("%s", ip), err
}

// GetHostName returns the host name part of an ip address.
func GetHostName(address string) string {
	parts := strings.Split(address, ":")
	return parts[0]
}

// GetPort returns the port name part of an ip address which includes a port number part.
func GetPort(address string) (string, error) {
	parts := strings.Split(address, ":")
	if len(parts) < 2 {
		return "", errors.New("missing port part in source address")
	}

	return parts[1], nil
}
