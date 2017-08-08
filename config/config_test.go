package config

import (
	"gopkg.in/yaml.v2"
	"testing"
	"time"
	"bytes"
	"fmt"
)

var expectedConf = Config{
	Cluster: Cluster{
		Name: "default cluster",
		Scheme: "http",
		Shards: []string{"localhost:8123"},
	},
	Users: []User{
		{
			Name: "web",
			MaxConcurrentQueries: 4,
			MaxExecutionTime: time.Duration(time.Minute),
		},
		{
			Name: "olap",
			MaxConcurrentQueries: 2,
			MaxExecutionTime: time.Duration(30*time.Second),
		},
	},
}

func TestLoadConfig(t *testing.T) {
	c, err := LoadFile("testdata/full.yml")
	if err != nil {
		t.Fatalf("Error parsing %s: %s", "testdata/full.yml", err)
	}

	if err := equalConfigs(c, &expectedConf); err != nil {
		t.Fatalf("%s:%s", "testdata/full.yml", err)
	}
}

var defaultValuesConf = Config{
	Cluster: Cluster{
		Name: "default cluster",
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
}

func TestDefaultValues(t * testing.T) {
	c, err := LoadFile("testdata/default.yml")
	if err != nil {
		t.Fatalf("Error parsing %s: %s", "testdata/default.yml", err)
	}

	if err := equalConfigs(c, &defaultValuesConf); err != nil {
		t.Fatalf("%s:%s", "testdata/default.yml", err)
	}


	c, err = LoadFile("testdata/default_user.yml")
	if err != nil {
		t.Fatalf("Error parsing %s: %s", "testdata/default_user.yml", err)
	}

	defaultValuesConf.Users = []User{
		{
			Name: "default",
		},
	}

	if err := equalConfigs(c, &defaultValuesConf); err != nil {
		t.Fatalf("%s:%s", "testdata/default_user.yml", err)
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
		return fmt.Errorf("unexpected config result: \n\n%s\n expected\n\n%s", bgot, bexp)
	}

	return nil
}