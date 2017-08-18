package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

var (
	defaultConfig = Config{
		Cluster: defaultCluster,
		Users:   defaultUsers,
	}

	defaultCluster = Cluster{
		Scheme: "http",
	}

	defaultUsers = []User{
		defaultUser,
	}

	defaultUser = User{
		Name: "default",
	}
)

// Config is an structure to describe CH cluster configuration
// The simplest configuration consists of:
// 	cluster description - see <remote_servers> section in CH config.xml
// 	and users - see <users> section in CH users.xml
type Config struct {
	Cluster Cluster `yaml:"cluster"`
	Users   []User  `yaml:"users,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// Validates passed configuration by additional marshalling
// to ensure that all rules and checks were applied
func (c *Config) Validate() error {
	content, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("error while marshalling config: %s", err)
	}

	cfg := &Config{}
	return yaml.Unmarshal([]byte(content), cfg)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = defaultConfig

	// set c to the defaults and then overwrite it with the input.
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if len(c.Users) == 0 {
		return fmt.Errorf("field `users` must contain at least 1 user")
	}

	return checkOverflow(c.XXX, "config")
}

// Cluster struct descrbes simplest <remote_servers> configuration
type Cluster struct {
	// Scheme: `http` or `https`; would be applied to all shards
	// default value is `http`
	Scheme string `yaml:"scheme,omitempty"`

	// Shards - list of shards addresses
	Shards []string `yaml:"shards"`

	// Catches all undefined fields
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

// User struct descrbes simplest <users> configuration
type User struct {
	// User name in ClickHouse users.xml config
	Name string `yaml:"user_name"`

	// Maximum number of concurrently running queries for user
	// if omitted or zero - no limits would be applied
	MaxConcurrentQueries uint32 `yaml:"max_concurrent_queries,omitempty"`

	// Maximum duration of query executing for user
	// if omitted or zero - no limits would be applied
	MaxExecutionTime time.Duration `yaml:"max_execution_time,omitempty"`

	// Catches all undefined fields
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

// Loads and validates configuration from provided .yml file
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
