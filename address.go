package main

import (
	"errors"
	"strconv"
	"strings"
)

func splitAddr(addr string) (host string, port int, err error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		err = errors.New("invalid address format")
		return
	}
	host = parts[0]
	port, err = strconv.Atoi(parts[1])
	if err != nil {
		err = errors.New("invalid port number")
		return
	}
	return
}
