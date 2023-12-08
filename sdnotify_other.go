//go:build !linux

package main

func sdNotifyReady() (bool, error) {
	return false, nil
}
