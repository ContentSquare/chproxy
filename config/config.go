package config

import (
	"time"
	"os"
	"fmt"
	"io/ioutil"
	"gopkg.in/yaml.v2"
	"strings"
)

var (
	DefaultConfig = Config{
		Cluster: DefaultCluster,
		Users: DefaultUsers,
	}

	DefaultCluster = Cluster{
		Scheme: "http",
	}

	DefaultUsers = []User{
		DefaultUser,
	}

	DefaultUser = User{
		Name: "default",
	}
)

type Config struct {
	Cluster Cluster  `yaml:"cluster"`
	Users []User  `yaml:"users,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig

	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if len(c.Users) == 0 {
		return fmt.Errorf("field `users` must contain at least 1 user")
	}

	return checkOverflow(c.XXX, "config")
}

type Cluster struct {
	Scheme string `yaml:"scheme,omitempty"`
	Shards []string `yaml:"shards"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Cluster) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Cluster
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if len(c.Shards) == 0 {
		return fmt.Errorf("field `shards` must contain at least 1 address")
	}

	if c.Scheme != "http" && c.Scheme != "https" {
		return fmt.Errorf("field `scheme` must be `http` or `https`. Got %q instead", c.Scheme)
	}

	return checkOverflow(c.XXX, "cluster")
}

type User struct {
	// User name in ClickHouse users.xml config
	Name                 string `yaml:"user_name"`
	// Maximum number of concurrently running queries for user
	MaxConcurrentQueries uint32 `yaml:"max_concurrent_queries,omitempty"`
	// Maximum duration of query executing for user
	MaxExecutionTime     time.Duration `yaml:"max_execution_time,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (u *User) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain User
	if err := unmarshal((*plain)(u)); err != nil {
		return err
	}

	return checkOverflow(u.XXX, "users")
}

func LoadFile(filename string) (*Config, error) {
	if stat, err := os.Stat(filename); err != nil {
		return nil, fmt.Errorf("cannot get file info: %s", err)
	} else if stat.IsDir() {
		return nil, fmt.Errorf("is a directory")
	}

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(content), cfg); err != nil {
		return nil, err
	}

	return cfg, nil
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
