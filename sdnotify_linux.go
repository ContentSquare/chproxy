//go:build linux

package main

import "github.com/coreos/go-systemd/v22/daemon"

func sdNotifyReady() (bool, error) {
	return daemon.SdNotify(false, daemon.SdNotifyReady)
}
