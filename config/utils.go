package config

import (
	"fmt"
	"net"
	"strings"
)

const entireIPv4 = "0.0.0.0/0"

func stringToIPnet(s string) (*net.IPNet, error) {
	if s == entireIPv4 {
		return nil, fmt.Errorf("suspicious mask specified \"0.0.0.0/0\". " +
			"If you want to allow all then just omit `allowed_networks` field")
	}
	ip := s
	if !strings.Contains(ip, `/`) {
		ip += "/32"
	}
	_, ipnet, err := net.ParseCIDR(ip)
	if err != nil {
		return nil, fmt.Errorf("wrong network group name or address %q: %s", s, err)
	}
	return ipnet, nil
}

func checkOverflow(m map[string]interface{}, ctx string) error {
	if len(m) > 0 {
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		return fmt.Errorf("unknown fields in %s: %s", ctx, strings.Join(keys, ", "))
	}
	return nil
}
