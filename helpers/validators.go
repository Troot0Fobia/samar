package helpers

import (
	"net"
	"strconv"
)

func ValidateIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

func ValidatePort(port string) bool {
	p, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return p > 0 && p <= 65535
}
