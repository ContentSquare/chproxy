package config

import (
	"time"
	"os"
	"fmt"
	"io/ioutil"
	"gopkg.in/yaml.v2"
	"strings"
)

type Config struct {
	Cluster Cluster  `yaml:"cluster"`
	Users []*User  `yaml:"users,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

type Cluster struct {
	Name string `yaml:"cluster_name"`
	Scheme string `yaml:"scheme,omitempty"`
	Shards []string `yaml:"shards"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

type User struct {
	// User name in ClickHouse users.xml config
	Name                 string `yaml:"user_name"`
	// Maximum number of concurrently running queries for user
	MaxConcurrentQueries int `yaml:"max_concurrent_queries,omitempty"`
	// Maximum duration of query executing for user
	MaxExecutionTime     time.Duration `yaml:"max_execution_time,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}


func Load(filename string) (*Config, error) {
	if stat, err := os.Stat(filename); err != nil {
		return nil, fmt.Errorf("cannot get file info: %s", err)
	} else if stat.IsDir() {
		return nil, fmt.Errorf("is a directory")
	}

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	cfg, err := newConfig(string(content))
	if err != nil {
		return nil, err
	}

	err = cfg.validate()
	return cfg, nil
}

func newConfig(s string) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(s), cfg); err != nil {
		return nil, err
	}

	if err := checkOverflow(cfg.XXX, "config"); err != nil {
		return nil, err
	}

	if err := checkOverflow(cfg.Cluster.XXX, "cluster"); err != nil {
		return nil, err
	}

	for _, user := range cfg.Users {
		if err := checkOverflow(user.XXX, "user"); err != nil {
			return nil, err
		}

		if err := checkOverflow(user.XXX); err != nil {
			return nil, err
		}

	}

	return cfg, nil
}

func (u *User) validate() error {
	_, err := time.ParseDuration(u.MaxExecutionTime)
	if err != nil {
		return err
	}

	return nil
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
