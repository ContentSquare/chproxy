package topology

import (
	"os"
	"testing"

	"github.com/contentsquare/chproxy/config"
)

func TestMain(m *testing.M) {
	cfg := &config.Config{
		Server: config.Server{
			Metrics: config.Metrics{
				Namespace: "test",
			},
		},
	}

	// Metrics should be preregistered to avoid nil-panics.
	RegisterMetrics(cfg)
	code := m.Run()
	os.Exit(code)
}
