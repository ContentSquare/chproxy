package config

import (
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	defaultConfig = Config{
		Clusters: []Cluster{defaultCluster},
	}

	defaultCluster = Cluster{
		Scheme:        "http",
		ClusterUsers:  []ClusterUser{defaultClusterUser},
		KillQueryUser: defaultKillQueryUser,
	}

	defaultKillQueryUser = KillQueryUser{
		Name: "default",
	}

	defaultClusterUser = ClusterUser{
		Name: "default",
	}
)

// Config describes access and proxy rules
type Config struct {
	Server Server `yaml:"server,omitempty"`

	Clusters []Cluster `yaml:"clusters"`

	Users []User `yaml:"users"`

	// Whether to print debug logs
	LogDebug bool `yaml:"log_debug,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
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

	if len(c.Server.HTTPS.ListenAddr) > 0 {
		if len(c.Server.HTTPS.Autocert.CacheDir) == 0 && len(c.Server.HTTPS.CertFile) == 0 && len(c.Server.HTTPS.KeyFile) == 0 {
			return fmt.Errorf("configuration `https` is missing. " +
				"Must be specified `https.cache_dir` for autocert " +
				"OR `https.key_file` and `https.cert_file` for already existing certs")
		}
	}

	return checkOverflow(c.XXX, "config")
}

// Server describes configuration of proxy server
// These settings are immutable and can't be reloaded without restart
type Server struct {
	// Optional HTTP configuration
	HTTP HTTP `yaml:"http,omitempty"`

	// Optional TLS configuration
	HTTPS HTTPS `yaml:"https,omitempty"`

	// Metrics optional metrics handler configuration
	Metrics Metrics `yaml:"metrics,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (s *Server) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Server
	if err := unmarshal((*plain)(s)); err != nil {
		return err
	}

	return checkOverflow(s.XXX, "server")
}

// HTTP describes configuration for server to listen HTTP connections
type HTTP struct {
	// TCP address to listen to for http
	// Default is `:8080`
	ListenAddr string `yaml:"listen_addr,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"allowed_networks,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *HTTP) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HTTP
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if len(c.ListenAddr) == 0 {
		c.ListenAddr = ":8080"
	}

	if len(c.AllowedNetworks) == 0 {
		c.AllowedNetworks = Networks{
			&net.IPNet{
				IP:   net.IPv4(127, 0, 0, 1),
				Mask: net.IPMask{255, 255, 255, 255},
			},
		}
	}

	return checkOverflow(c.XXX, "http")
}

// HTTPS describes configuration for server to listen HTTPS connections
// It can be autocert with letsencrypt
// or custom certificate
type HTTPS struct {
	// TCP address to listen to for https
	// Default is `:443`
	ListenAddr string `yaml:"listen_addr,omitempty"`

	// Certificate and key files for client cert authentication to the server
	CertFile string `yaml:"cert_file,omitempty"`
	KeyFile  string `yaml:"key_file,omitempty"`

	Autocert Autocert `yaml:"autocert,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"allowed_networks,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *HTTPS) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HTTPS
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if len(c.ListenAddr) == 0 {
		c.ListenAddr = ":443"
	}

	if len(c.Autocert.CacheDir) > 0 && (len(c.CertFile) > 0 || len(c.KeyFile) > 0) {
		return fmt.Errorf("it is forbidden to specify certificate and `https.autocert` at the same time. Choose one way")
	}

	if len(c.CertFile) > 0 && len(c.KeyFile) == 0 {
		return fmt.Errorf("field `https.key_file` must be specified")
	}

	if len(c.KeyFile) > 0 && len(c.CertFile) == 0 {
		return fmt.Errorf("field `https.cert_file` must be specified")
	}

	return checkOverflow(c.XXX, "https")
}

// Autocert configuration via letsencrypt
type Autocert struct {
	// Path to the directory where autocert certs are cached
	CacheDir string `yaml:"cache_dir,omitempty"`

	// List of host names to which proxy is allowed to respond to
	// see https://godoc.org/golang.org/x/crypto/acme/autocert#HostPolicy
	AllowedHosts []string `yaml:"allowed_hosts,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Autocert) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Autocert
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	return checkOverflow(c.XXX, "autocert")
}

type Metrics struct {
	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"allowed_networks,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Metrics) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Metrics
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	return checkOverflow(c.XXX, "metrics")
}

// Cluster describes CH cluster configuration
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

	// KillQueryUser - user configuration for killing
	// queries which has exceeded limits
	// if not specified - killing queries will be omitted
	KillQueryUser KillQueryUser `yaml:"kill_query_user,omitempty"`

	// HeartBeatInterval is an interval of checking
	// all cluster nodes for availability
	// if omitted or zero - interval will be set to 5s
	HeartBeatInterval time.Duration `yaml:"heartbeat_interval,omitempty"`

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

	if c.HeartBeatInterval == 0 {
		c.HeartBeatInterval = time.Second * 5
	}

	return checkOverflow(c.XXX, "cluster")
}

// KillQueryUser - user configuration for killing
// queries which has exceeded limits
type KillQueryUser struct {
	// User name
	Name string `yaml:"name"`

	// User password to access CH with basic auth
	Password string `yaml:"password,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (u *KillQueryUser) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain KillQueryUser
	if err := unmarshal((*plain)(u)); err != nil {
		return err
	}

	if len(u.Name) == 0 {
		return fmt.Errorf("field `cluster.kill_query_user.name` must be specified")
	}

	return checkOverflow(u.XXX, "kill_query_user")
}

// User describes list of allowed users
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
	AllowedNetworks Networks `yaml:"allowed_networks,omitempty"`

	// Whether to deny http connections for this user
	DenyHTTP bool `yaml:"deny_http,omitempty"`

	// Whether to deny https connections for this user
	DenyHTTPS bool `yaml:"deny_https,omitempty"`

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
		return fmt.Errorf("field `user.to_user` for %q cannot be empty", u.Name)
	}

	if len(u.ToCluster) == 0 {
		return fmt.Errorf("field `user.to_cluster` for %q cannot be empty", u.Name)
	}

	if len(u.Password) == 0 && len(u.AllowedNetworks) == 0 {
		return fmt.Errorf("access for user %q must be limited by `password` or by `allowed_networks`", u.Name)
	}

	if u.DenyHTTP && u.DenyHTTPS {
		return fmt.Errorf("user %q has both `deny_http` and `deny_https` set to `true`", u.Name)
	}

	if u.DenyHTTP == false && len(u.AllowedNetworks) == 0 {
		return fmt.Errorf("user %q is allowed to connect via http but not limited by `allowed_networks` - possible security breach", u.Name)
	}

	return checkOverflow(u.XXX, "user")
}

// Networks is a list of IPNet entities
type Networks []*net.IPNet

const entireIPv4 = "0.0.0.0/0"

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (n *Networks) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var sl []string
	if err := unmarshal(&sl); err != nil {
		return err
	}
	networks := make(Networks, len(sl))
	for i, s := range sl {
		if s == entireIPv4 {
			return fmt.Errorf("suspicious mask specified \"0.0.0.0/0\". " +
				"If you want to allow all then just omit `allowed_networks` field")
		}

		if !strings.Contains(s, `/`) {
			s += "/32"
		}

		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return err
		}

		networks[i] = ipnet
	}

	*n = networks

	return nil
}

// Contains checks whether passed addr is in the range of networks
func (n Networks) Contains(addr string) bool {
	if len(n) == 0 {
		return true
	}

	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		panic(fmt.Sprintf("BUG: unexpected error while parsing RemoteAddr: %s", err))
	}

	ip := net.ParseIP(h)
	if ip == nil {
		panic(fmt.Sprintf("BUG: unexpected error while parsing IP: %s", h))
	}

	for _, ipnet := range n {
		if ipnet.Contains(ip) {
			return true
		}
	}

	return false
}

// ClusterUser describes simplest <users> configuration
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

// LoadFile loads and validates configuration from provided .yml file
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
