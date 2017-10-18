package config

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type ByteSize float64

const (
	_           = iota
	KB ByteSize = 1 << (10 * iota)
	MB
	GB
	TB
)

var (
	bytesPattern   *regexp.Regexp = regexp.MustCompile(`(?i)^(-?\d+(?:\.\d+)?)([KMGT]B?|B)$`)
	errInvalidSize                = errors.New("wrong size format: must be a positive integer with a unit of measurement like M, MB, G, GB, T or TB")
)

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (ds *ByteSize) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	parts := bytesPattern.FindStringSubmatch(strings.TrimSpace(s))
	if len(parts) < 3 {
		return errInvalidSize
	}

	value, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || value <= 0 {
		return errInvalidSize
	}

	unit := strings.ToUpper(parts[2])
	switch unit[:1] {
	case "T", "TB":
		*ds = ByteSize(value) * TB
	case "G", "GB":
		*ds = ByteSize(value) * GB
	case "M", "MB":
		*ds = ByteSize(value) * MB
	case "K", "KB":
		*ds = ByteSize(value) * KB
	}

	return nil
}

// Networks is a list of IPNet entities
type Networks []*net.IPNet

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (n *Networks) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s []string
	if err := unmarshal(&s); err != nil {
		return err
	}
	networks := make(Networks, len(s))
	for i, s := range s {
		ipnet, err := stringToIPnet(s)
		if err != nil {
			return err
		}
		networks[i] = ipnet
	}
	*n = networks
	return nil
}

// Contains checks whether passed addr is in the range of networks
func (n Networks) Contains(addr string) bool {
	if len(n) == 0 {
		return true
	}

	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		panic(fmt.Sprintf("BUG: unexpected error while parsing RemoteAddr: %s", err))
	}

	ip := net.ParseIP(h)
	if ip == nil {
		panic(fmt.Sprintf("BUG: unexpected error while parsing IP: %s", h))
	}

	for _, ipnet := range n {
		if ipnet.Contains(ip) {
			return true
		}
	}

	return false
}
