package config

import (
	"gopkg.in/yaml.v2"
	"testing"
	"time"
	"bytes"
	"fmt"
)

type testCase struct {
	file string
	cfg Config
}

var testCases = []testCase{
	{
		file: "testdata/full.yml",
		cfg: Config{
			Cluster: Cluster{
				Scheme: "http",
				Shards: []string{"localhost:8123"},
			},
			Users: []User{
				{
					Name:                 "web",
					MaxConcurrentQueries: 4,
					MaxExecutionTime:     time.Duration(time.Minute),
				},
				{
					Name:                 "olap",
					MaxConcurrentQueries: 2,
					MaxExecutionTime:     time.Duration(30 * time.Second),
				},
			},
		},
	},
	{
		file: "testdata/default_limits.yml",
		cfg: Config{
			Cluster: Cluster{
				Scheme: "http",
				Shards: []string{"localhost:8123"},
			},
			Users: []User{
				{
					Name: "web",
				},
				{
					Name: "olap",
				},
			},
		},
	},
	{
		file: "testdata/default_user.yml",
		cfg: Config{
			Cluster: Cluster{
				Scheme: "http",
				Shards: []string{"localhost:8123"},
			},
			Users: []User{
				{
					Name: "default",
				},
			},
		},
	},
}

func TestLoadConfig(t *testing.T) {
	for _, tc := range testCases {
		c, err := LoadFile(tc.file)
		if err != nil {
			t.Fatalf("Error parsing %s: %s", tc.file, err)
		}

		if err := equalConfigs(c, &tc.cfg); err != nil {
			t.Fatalf("%s:%s", tc.file, err)
		}
	}

}

func equalConfigs(a, b *Config) error {
	bgot, err := yaml.Marshal(a)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	bexp, err := yaml.Marshal(b)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	if !bytes.Equal(bgot, bexp) {
		return fmt.Errorf("unexpected config result: \ngot\n\n%s\n expected\n\n%s", bgot, bexp)
	}

	return nil
}