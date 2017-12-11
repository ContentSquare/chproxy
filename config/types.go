package config

import (
	"fmt"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ByteSize holds size in bytes.
//
// May be used in yaml for parsing byte size values.
type ByteSize uint64

var byteSizeRegexp = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([KMGTP]?)B?$`)

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (bs *ByteSize) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	s = strings.ToUpper(s)

	parts := byteSizeRegexp.FindStringSubmatch(strings.TrimSpace(s))
	if len(parts) < 3 {
		return fmt.Errorf("cannot parse byte size %q: it must be positive float followed by optional units. For example, 1.5Gb, 3T", s)
	}

	value, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("cannot parse byte size %q: it must be positive float followed by optional units. For example, 1.5Gb, 3T; err: %s", s, err)
	}
	if value <= 0 {
		return fmt.Errorf("byte size %q must be positive", s)
	}

	k := float64(1)
	unit := parts[2]
	switch unit {
	case "P":
		k = 1 << 50
	case "T":
		k = 1 << 40
	case "G":
		k = 1 << 30
	case "M":
		k = 1 << 20
	case "K":
		k = 1 << 10
	}

	value *= k
	*bs = ByteSize(value)

	// check for overflow
	e := math.Abs(float64(*bs)-value) / value
	if e > 1e-6 {
		return fmt.Errorf("byte size %q is too big", s)
	}

	return nil
}

// Networks is a list of IPNet entities
type Networks []*net.IPNet

// MarshalYAML implements yaml.Marshaler interface.
//
// It prettifies yaml output for Networks.
func (n Networks) MarshalYAML() (interface{}, error) {
	var a []string
	for _, x := range n {
		a = append(a, x.String())
	}
	return a, nil
}

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

// Duration wraps time.Duration. It is used to parse the custom duration format
type Duration time.Duration

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	dur, err := parseDuration(s)
	if err != nil {
		return err
	}
	*d = dur
	return nil
}

func (d Duration) String() string {
	factors := map[string]time.Duration{
		"w":  time.Hour * 24 * 7,
		"d":  time.Hour * 24,
		"h":  time.Hour,
		"m":  time.Minute,
		"s":  time.Second,
		"ms": time.Millisecond,
		"µs": time.Microsecond,
		"ns": 1,
	}

	var t = time.Duration(d)
	unit := "ns"
	switch time.Duration(0) {
	case t % factors["w"]:
		unit = "w"
	case t % factors["d"]:
		unit = "d"
	case t % factors["h"]:
		unit = "h"
	case t % factors["m"]:
		unit = "m"
	case t % factors["s"]:
		unit = "s"
	case t % factors["ms"]:
		unit = "ms"
	case t % factors["µs"]:
		unit = "µs"
	}
	return fmt.Sprintf("%d%v", t/factors[unit], unit)
}

// MarshalYAML implements the yaml.Marshaler interface.
func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}

// borrowed from github.com/prometheus/prometheus
var durationRE = regexp.MustCompile("^([0-9]+)(w|d|h|m|s|ms|µs|ns)$")

// StringToDuration parses a string into a time.Duration, assuming that a year
// always has 365d, a week always has 7d, and a day always has 24h.
func parseDuration(durationStr string) (Duration, error) {
	matches := durationRE.FindStringSubmatch(durationStr)
	if len(matches) != 3 {
		return 0, fmt.Errorf("not a valid duration string: %q", durationStr)
	}
	var n, _ = strconv.Atoi(matches[1])
	var dur = time.Duration(n)
	switch unit := matches[2]; unit {
	case "w":
		dur *= time.Hour * 24 * 7
	case "d":
		dur *= time.Hour * 24
	case "h":
		dur *= time.Hour
	case "m":
		dur *= time.Minute
	case "s":
		dur *= time.Second
	case "ms":
		dur *= time.Millisecond
	case "µs":
		dur *= time.Microsecond
	case "ns":
	default:
		return 0, fmt.Errorf("invalid time unit in duration string: %q", unit)
	}
	return Duration(dur), nil
}
