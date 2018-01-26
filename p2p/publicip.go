package p2p

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

// GetPublicIpAddrtess returns this host public ip address.
// Method is implemented using the ipify.org service.
func GetPublicIpAddress() (string, error) {

	// todo: make this more robust by adding additional fallback services so
	// if ipify.org can't bring down our whole network

	res, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}

	ip, err := ioutil.ReadAll(res.Body)
	return fmt.Sprintf("%s", ip), err
}

func GetHostName(address string) string {
	parts := strings.Split(address, ":")
	return parts[0]
}


func GetPort(address string) string {
	parts := strings.Split(address, ":")
	return parts[1]
}