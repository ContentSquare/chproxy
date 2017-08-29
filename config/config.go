package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"strings"
	"time"
)

var (
	defaultConfig = Config{
		ListenAddr: ":8080",
		Clusters:   []Cluster{defaultCluster},
	}

	defaultCluster = Cluster{
		Scheme:         "http",
		ClusterUsers: []ClusterUser{defaultClusterUser},
	}

	defaultClusterUser = ClusterUser{
		Name: "default",
	}
)

// Config is an structure to describe access and proxy rules
type Config struct {
	// TCP address to listen to for http
	// Default is `localhost:8080`
	ListenAddr string `yaml:"listen_addr,omitempty"`

	// TCP address to listen to for https
	ListenTLSAddr string `yaml:"listen_tls_addr,omitempty"`

	// Path to the directory where letsencrypt certs are cached
	CertCacheDir string `yaml:"cert_cache_dir,omitempty"`

	Clusters []Cluster `yaml:"clusters"`

	Users []User `yaml:"users"`

	// Whether to print debug logs
	LogDebug bool `yaml:"log_debug,omitempty"`

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

	if len(c.Clusters) == 0 {
		return fmt.Errorf("field `clusters` must contain at least 1 cluster")
	}

	if len(c.ListenTLSAddr) > 0 && len(c.CertCacheDir) == 0 {
		return fmt.Errorf("field `cert_cache_dir` must be set for TLS")
	}

	return checkOverflow(c.XXX, "config")
}

// Cluster is an structure to describe CH cluster configuration
// The simplest configuration consists of:
// 	 cluster description - see <remote_servers> section in CH config.xml
// 	 and users - see <users> section in CH users.xml
type Cluster struct {
	// Name of ClickHouse cluster
	Name string `yaml:"name"`

	// Scheme: `http` or `https`; would be applied to all nodes
	// default value is `http`
	Scheme string `yaml:"scheme,omitempty"`

	// Nodes - list of nodes addresses
	Nodes []string `yaml:"nodes"`

	// ClusterUsers - list of ClickHouse users
	ClusterUsers []ClusterUser `yaml:"users"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Cluster) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = defaultCluster

	type plain Cluster
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if len(c.Name) == 0 {
		return fmt.Errorf("field `cluster.name` cannot be empty")
	}

	if len(c.Nodes) == 0 {
		return fmt.Errorf("field `cluster.nodes` must contain at least 1 address")
	}

	if len(c.ClusterUsers) == 0 {
		return fmt.Errorf("field `cluster.users` must contain at least 1 user")
	}

	if c.Scheme != "http" && c.Scheme != "https" {
		return fmt.Errorf("field `cluster.scheme` must be `http` or `https`. Got %q instead", c.Scheme)
	}

	return checkOverflow(c.XXX, "cluster")
}

// User struct describes list of allowed users
// which requests will be proxied to ClickHouse
type User struct {
	// User name
	Name string `yaml:"name"`

	// User password to access proxy with basic auth
	Password string `yaml:"password,omitempty"`

	// ToCluster is the name of cluster where requests
	// will be proxied
	ToCluster string `yaml:"to_cluster"`

	// ToUser is the name of cluster_user from cluster's ToCluster
	// whom credentials will be used for proxying request to CH
	ToUser string `yaml:"to_user"`

	// Maximum number of concurrently running queries for user
	// if omitted or zero - no limits would be applied
	MaxConcurrentQueries uint32 `yaml:"max_concurrent_queries,omitempty"`

	// Maximum duration of query execution for user
	// if omitted or zero - no limits would be applied
	MaxExecutionTime time.Duration `yaml:"max_execution_time,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks []string `yaml:"allowed_networks,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (u *User) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain User
	if err := unmarshal((*plain)(u)); err != nil {
		return err
	}

	if len(u.Name) == 0 {
		return fmt.Errorf("field `user.name` cannot be empty")
	}

	if len(u.ToUser) == 0 {
		return fmt.Errorf("field `user.to_user` cannot be empty")
	}

	if len(u.ToCluster) == 0 {
		return fmt.Errorf("field `user.to_cluster` cannot be empty")
	}

	return checkOverflow(u.XXX, "user")
}

// User struct describes simplest <users> configuration
type ClusterUser struct {
	// User name in ClickHouse users.xml config
	Name string `yaml:"name"`

	// User password in ClickHouse users.xml config
	Password string `yaml:"password,omitempty"`

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
func (u *ClusterUser) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ClusterUser
	if err := unmarshal((*plain)(u)); err != nil {
		return err
	}

	if len(u.Name) == 0 {
		return fmt.Errorf("field `cluster.user.name` cannot be empty")
	}

	return checkOverflow(u.XXX, "cluster.users")
}

// Loads and validates configuration from provided .yml file
func LoadFile(filename string) (*Config, error) {
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
