package cache

import (
	"github.com/Vertamedia/chproxy/log"
	"os"
	"testing"
)

var testDir = "./test-data"

func TestMain(m *testing.M) {
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		os.Mkdir(testDir, os.ModePerm)
	}

	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)

	if err := os.RemoveAll(testDir); err != nil {
		log.Fatalf("cannot remove %q: %s", testDir, err)
	}
	os.Exit(retCode)
}
