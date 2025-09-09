package config

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mohae/deepcopy"
	"golang.org/x/crypto/acme/autocert"
	"gopkg.in/yaml.v2"
)

var (
	defaultConfig = Config{
		Clusters: []Cluster{defaultCluster},
	}

	defaultCluster = Cluster{
		Scheme:       "http",
		ClusterUsers: []ClusterUser{defaultClusterUser},
		HeartBeat:    defaultHeartBeat,
		RetryNumber:  defaultRetryNumber,
	}

	defaultClusterUser = ClusterUser{
		Name: "default",
	}

	defaultHeartBeat = HeartBeat{
		Interval: Duration(time.Second * 5),
		Timeout:  Duration(time.Second * 3),
		Request:  "/ping",
		Response: "Ok.\n",
		User:     "",
		Password: "",
	}

	defaultConnectionPool = ConnectionPool{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 2,
	}

	defaultExecutionTime = Duration(120 * time.Second)

	defaultMaxPayloadSize = ByteSize(1 << 50)

	defaultMaxErrorReasonSize = ByteSize(1 << 50)

	defaultRetryNumber = 0
)

// Config describes server configuration, access and proxy rules
type Config struct {
	Server Server `yaml:"server,omitempty"`

	Clusters []Cluster `yaml:"clusters"`

	Users []User `yaml:"users"`

	// Whether to print debug logs
	LogDebug bool `yaml:"log_debug,omitempty"`

	LogMasks []LogMask `yaml:"log_masks,omitempty"`

	// Whether to ignore security warnings
	HackMePlease bool `yaml:"hack_me_please,omitempty"`

	NetworkGroups []NetworkGroups `yaml:"network_groups,omitempty"`

	// Maximum size of error payload
	MaxErrorReasonSize ByteSize `yaml:"max_error_reason_size,omitempty"`

	Caches []Cache `yaml:"caches,omitempty"`

	ParamGroups []ParamGroup `yaml:"param_groups,omitempty"`

	ConnectionPool ConnectionPool `yaml:"connection_pool,omitempty"`

	// Allow to proxy ping requests
	AllowPing bool `yaml:"allow_ping,omitempty"`

	networkReg map[string]Networks

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// String implements the Stringer interface
func (c *Config) String() string {
	b, err := yaml.Marshal(withoutSensitiveInfo(c))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func withoutSensitiveInfo(config *Config) *Config {
	const pswPlaceHolder = "XXX"

	// nolint: forcetypeassert // no need to check type, it is specified by function.
	c := deepcopy.Copy(config).(*Config)
	for i := range c.Users {
		c.Users[i].Password = pswPlaceHolder
	}
	for i := range c.Clusters {
		if len(c.Clusters[i].HeartBeat.User) > 0 {
			c.Clusters[i].HeartBeat.Password = pswPlaceHolder
		}
		if len(c.Clusters[i].KillQueryUser.Name) > 0 {
			c.Clusters[i].KillQueryUser.Password = pswPlaceHolder
		}
		for j := range c.Clusters[i].ClusterUsers {
			c.Clusters[i].ClusterUsers[j].Password = pswPlaceHolder
		}
	}
	for i := range c.Caches {
		if len(c.Caches[i].Redis.Password) > 0 {
			c.Caches[i].Redis.Password = pswPlaceHolder
		}
	}
	return c
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// set c to the defaults and then overwrite it with the input.
	*c = defaultConfig
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if err := c.validate(); err != nil {
		return err
	}

	return checkOverflow(c.XXX, "config")
}

func (c *Config) validate() error {
	if len(c.Users) == 0 {
		return fmt.Errorf("`users` must contain at least 1 user")
	}

	if len(c.Clusters) == 0 {
		return fmt.Errorf("`clusters` must contain at least 1 cluster")
	}

	if len(c.Server.HTTP.ListenAddr) == 0 && len(c.Server.HTTPS.ListenAddr) == 0 {
		return fmt.Errorf("neither HTTP nor HTTPS not configured")
	}

	if len(c.Server.HTTPS.ListenAddr) > 0 {
		if len(c.Server.HTTPS.Autocert.CacheDir) == 0 && len(c.Server.HTTPS.CertFile) == 0 && len(c.Server.HTTPS.KeyFile) == 0 {
			return fmt.Errorf("configuration `https` is missing. " +
				"Must be specified `https.cache_dir` for autocert " +
				"OR `https.key_file` and `https.cert_file` for already existing certs")
		}
		if len(c.Server.HTTPS.Autocert.CacheDir) > 0 {
			c.Server.HTTP.ForceAutocertHandler = true
		}
	}
	return nil
}

func (cfg *Config) setDefaults() error {
	var maxResponseTime time.Duration
	var err error
	for i := range cfg.Clusters {
		c := &cfg.Clusters[i]
		for j := range c.ClusterUsers {
			u := &c.ClusterUsers[j]
			cud := time.Duration(u.MaxExecutionTime + u.MaxQueueTime)
			if cud > maxResponseTime {
				maxResponseTime = cud
			}
			if u.AllowedNetworks, err = cfg.groupToNetwork(u.NetworksOrGroups); err != nil {
				return err
			}
		}
	}
	for i := range cfg.Users {
		u := &cfg.Users[i]
		u.setDefaults()

		ud := time.Duration(u.MaxExecutionTime + u.MaxQueueTime)
		if ud > maxResponseTime {
			maxResponseTime = ud
		}
		if u.AllowedNetworks, err = cfg.groupToNetwork(u.NetworksOrGroups); err != nil {
			return err
		}
	}

	for i := range cfg.Caches {
		c := &cfg.Caches[i]
		c.setDefaults()
	}

	if cfg.MaxErrorReasonSize <= 0 {
		cfg.MaxErrorReasonSize = defaultMaxErrorReasonSize
	}

	cfg.setServerMaxResponseTime(maxResponseTime)

	return nil
}

func (cfg *Config) setServerMaxResponseTime(maxResponseTime time.Duration) {
	if maxResponseTime < 0 {
		maxResponseTime = 0
	}

	// Give an additional minute for the maximum response time,
	// so the response body may be sent to the requester.
	maxResponseTime += time.Minute
	if len(cfg.Server.HTTP.ListenAddr) > 0 && cfg.Server.HTTP.WriteTimeout == 0 {
		cfg.Server.HTTP.WriteTimeout = Duration(maxResponseTime)
	}

	if len(cfg.Server.HTTPS.ListenAddr) > 0 && cfg.Server.HTTPS.WriteTimeout == 0 {
		cfg.Server.HTTPS.WriteTimeout = Duration(maxResponseTime)
	}
}

func (c *Config) groupToNetwork(src NetworksOrGroups) (Networks, error) {
	if len(src) == 0 {
		return nil, nil
	}

	dst := make(Networks, 0)
	for _, v := range src {
		group, ok := c.networkReg[v]
		if ok {
			dst = append(dst, group...)
		} else {
			ipnet, err := stringToIPnet(v)
			if err != nil {
				return nil, err
			}
			dst = append(dst, ipnet)
		}
	}
	return dst, nil
}

// Server describes configuration of proxy server
// These settings are immutable and can't be reloaded without restart
type Server struct {
	// Optional HTTP configuration
	HTTP HTTP `yaml:"http,omitempty"`

	// Optional TLS configuration
	HTTPS HTTPS `yaml:"https,omitempty"`

	// Optional metrics handler configuration
	Metrics Metrics `yaml:"metrics,omitempty"`

	// Optional Proxy configuration
	Proxy Proxy `yaml:"proxy,omitempty"`

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

// TimeoutCfg contains configurable http.Server timeouts
type TimeoutCfg struct {
	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	// Default value is 1m
	ReadTimeout Duration `yaml:"read_timeout,omitempty"`

	// WriteTimeout is the maximum duration before timing out writes of the response.
	// Default is largest MaxExecutionTime + MaxQueueTime value from Users or Clusters
	WriteTimeout Duration `yaml:"write_timeout,omitempty"`

	// IdleTimeout is the maximum amount of time to wait for the next request.
	// Default is 10m
	IdleTimeout Duration `yaml:"idle_timeout,omitempty"`
}

// HTTP describes configuration for server to listen HTTP connections
type HTTP struct {
	// TCP address to listen to for http
	ListenAddr string `yaml:"listen_addr"`

	NetworksOrGroups NetworksOrGroups `yaml:"allowed_networks,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"-"`

	// Whether to support Autocert handler for http-01 challenge
	ForceAutocertHandler bool

	TimeoutCfg `yaml:",inline"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *HTTP) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HTTP
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if err := c.validate(); err != nil {
		return err
	}

	return checkOverflow(c.XXX, "http")
}

func (c *HTTP) validate() error {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = Duration(time.Minute)
	}

	if c.IdleTimeout == 0 {
		c.IdleTimeout = Duration(time.Minute * 10)
	}

	return nil
}

// TLS describes generic configuration for TLS connections,
// it can be used for both HTTPS and Redis TLS.
type TLS struct {
	// Certificate and key files for client cert authentication to the server
	CertFile           string   `yaml:"cert_file,omitempty"`
	KeyFile            string   `yaml:"key_file,omitempty"`
	Autocert           Autocert `yaml:"autocert,omitempty"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify,omitempty"`
}

// BuildTLSConfig builds tls.Config from TLS configuration.
func (c *TLS) BuildTLSConfig(acm *autocert.Manager) (*tls.Config, error) {
	tlsCfg := tls.Config{
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
		InsecureSkipVerify: c.InsecureSkipVerify, // nolint: gosec
	}
	if len(c.KeyFile) > 0 && len(c.CertFile) > 0 {
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("cannot load cert for `cert_file`=%q, `key_file`=%q: %w",
				c.CertFile, c.KeyFile, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	} else if acm != nil {
		tlsCfg.GetCertificate = acm.GetCertificate
	}

	return &tlsCfg, nil
}

// HTTPS describes configuration for server to listen HTTPS connections
// It can be autocert with letsencrypt
// or custom certificate
type HTTPS struct {
	// TCP address to listen to for https
	// Default is `:443`
	ListenAddr string `yaml:"listen_addr,omitempty"`

	// TLS configuration
	TLS `yaml:",inline"`

	NetworksOrGroups NetworksOrGroups `yaml:"allowed_networks,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"-"`

	TimeoutCfg `yaml:",inline"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *HTTPS) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HTTPS
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if err := c.validate(); err != nil {
		return err
	}

	return checkOverflow(c.XXX, "https")
}

func (c *HTTPS) validate() error {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = Duration(time.Minute)
	}

	if c.IdleTimeout == 0 {
		c.IdleTimeout = Duration(time.Minute * 10)
	}

	if len(c.ListenAddr) == 0 {
		c.ListenAddr = ":443"
	}

	if err := c.validateCertConfig(); err != nil {
		return err
	}

	return nil
}

func (c *HTTPS) validateCertConfig() error {
	if len(c.Autocert.CacheDir) > 0 {
		if len(c.CertFile) > 0 || len(c.KeyFile) > 0 {
			return fmt.Errorf("it is forbidden to specify certificate and `https.autocert` at the same time. Choose one way")
		}
		if len(c.NetworksOrGroups) > 0 {
			return fmt.Errorf("`letsencrypt` specification requires https server to be without `allowed_networks` limits. " +
				"Otherwise, certificates will be impossible to generate")
		}
	}

	if len(c.CertFile) > 0 && len(c.KeyFile) == 0 {
		return fmt.Errorf("`https.key_file` must be specified")
	}

	if len(c.KeyFile) > 0 && len(c.CertFile) == 0 {
		return fmt.Errorf("`https.cert_file` must be specified")
	}

	return nil
}

// Autocert configuration via letsencrypt
// It requires port :80 to be open
// see https://community.letsencrypt.org/t/2018-01-11-update-regarding-acme-tls-sni-and-shared-hosting-infrastructure/50188
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

// Metrics describes configuration to access metrics endpoint
type Metrics struct {
	NetworksOrGroups NetworksOrGroups `yaml:"allowed_networks,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"-"`

	// Prometheus metric namespace
	Namespace string `yaml:"namespace,omitempty"`

	// Constant labels which will be added to each sent prometheus metric
	ConstantLabels map[string]string `yaml:"constant_labels,omitempty"`

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

type Proxy struct {
	// Enable enables parsing proxy headers. In proxy mode, CHProxy will try to
	// parse the X-Forwarded-For, X-Real-IP or Forwarded header to extract the IP. If an other header is configured
	// in the proxy settings, CHProxy will  use that header instead.
	Enable bool `yaml:"enable,omitempty"`

	// Header allows for configuring an alternative header to parse the remote IP from, e.g.
	// CF-Connecting-IP. If this is set, Enable must be set to true otherwise this setting
	// will be ignored.
	Header string `yaml:"header,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Proxy) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Proxy
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if !c.Enable && c.Header != "" {
		return fmt.Errorf("`proxy_header` cannot be set without enabling proxy settings")
	}

	return checkOverflow(c.XXX, "proxy")
}

// Cluster describes CH cluster configuration
// The simplest configuration consists of:
//
//	cluster description - see <remote_servers> section in CH config.xml
//	and users - see <users> section in CH users.xml
type Cluster struct {
	// Name of ClickHouse cluster
	Name string `yaml:"name"`

	// Scheme: `http` or `https`; would be applied to all nodes
	// default value is `http`
	Scheme string `yaml:"scheme,omitempty"`

	// Nodes contains cluster nodes.
	//
	// Either Nodes or Replicas must be set, but not both.
	Nodes []string `yaml:"nodes,omitempty"`

	// Replicas contains replicas.
	//
	// Either Replicas or Nodes must be set, but not both.
	Replicas []Replica `yaml:"replicas,omitempty"`

	// ClusterUsers - list of ClickHouse users
	ClusterUsers []ClusterUser `yaml:"users"`

	// KillQueryUser - user configuration for killing timed out queries.
	// By default timed out queries are killed under `default` user.
	KillQueryUser KillQueryUser `yaml:"kill_query_user,omitempty"`

	// HeartBeat - user configuration for heart beat requests
	HeartBeat HeartBeat `yaml:"heartbeat,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`

	// Retry number for query - how many times a query can retry after receiving a recoverable but failed response from Clickhouse node
	RetryNumber int `yaml:"retry_number,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Cluster) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = defaultCluster
	type plain Cluster
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if err := c.validate(); err != nil {
		return err
	}

	return checkOverflow(c.XXX, fmt.Sprintf("cluster %q", c.Name))
}

func (c *Cluster) validate() error {
	if len(c.Name) == 0 {
		return fmt.Errorf("`cluster.name` cannot be empty")
	}

	if err := c.validateMinimumRequirements(); err != nil {
		return err
	}

	if c.Scheme != "http" && c.Scheme != "https" {
		return fmt.Errorf("`cluster.scheme` must be `http` or `https`, got %q instead for %q", c.Scheme, c.Name)
	}

	if c.HeartBeat.Interval == 0 && c.HeartBeat.Timeout == 0 && c.HeartBeat.Response == "" {
		return fmt.Errorf("`cluster.heartbeat` cannot be unset for %q", c.Name)
	}

	return nil
}

func (c *Cluster) validateMinimumRequirements() error {
	if len(c.Nodes) == 0 && len(c.Replicas) == 0 {
		return fmt.Errorf("either `cluster.nodes` or `cluster.replicas` must be set for %q", c.Name)
	}

	if len(c.Nodes) > 0 && len(c.Replicas) > 0 {
		return fmt.Errorf("`cluster.nodes` cannot be simultaneously set with `cluster.replicas` for %q", c.Name)
	}

	if len(c.ClusterUsers) == 0 {
		return fmt.Errorf("`cluster.users` must contain at least 1 user for %q", c.Name)
	}

	return nil
}

// Replica contains ClickHouse replica configuration.
type Replica struct {
	// Name is replica name.
	Name string `yaml:"name"`

	// Nodes contains replica nodes.
	Nodes []string `yaml:"nodes"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (r *Replica) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Replica
	if err := unmarshal((*plain)(r)); err != nil {
		return err
	}
	if len(r.Name) == 0 {
		return fmt.Errorf("`replica.name` cannot be empty")
	}
	if len(r.Nodes) == 0 {
		return fmt.Errorf("`replica.nodes` cannot be empty for %q", r.Name)
	}
	return checkOverflow(r.XXX, fmt.Sprintf("replica %q", r.Name))
}

// KillQueryUser - user configuration for killing timed out queries.
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
		return fmt.Errorf("`cluster.kill_query_user.name` must be specified")
	}
	return checkOverflow(u.XXX, "kill_query_user")
}

// HeartBeat - configuration for heartbeat.
type HeartBeat struct {
	// Interval is an interval of checking
	// all cluster nodes for availability
	// if omitted or zero - interval will be set to 5s
	Interval Duration `yaml:"interval,omitempty"`

	// Timeout is a timeout of wait response from cluster nodes
	// if omitted or zero - interval will be set to 3s
	Timeout Duration `yaml:"timeout,omitempty"`

	// Request is a query
	// default value is `/ping`
	Request string `yaml:"request,omitempty"`

	// Reference response from clickhouse on health check request
	// default value is `Ok.\n`
	Response string `yaml:"response,omitempty"`

	// Credentials to send heartbeat requests
	// for anything except '/ping'.
	// If not specified, the first cluster user' creadentials are used
	User     string `yaml:"user,omitempty"`
	Password string `yaml:"password,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (h *HeartBeat) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HeartBeat
	if err := unmarshal((*plain)(h)); err != nil {
		return err
	}
	return checkOverflow(h.XXX, "heartbeat")
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
	// if omitted or zero - limit is set to 120 seconds
	MaxExecutionTime Duration `yaml:"max_execution_time,omitempty"`

	// Maximum number of requests per minute for user
	// if omitted or zero - no limits would be applied
	// if negative - the user is effectively blocked
	ReqPerMin int32 `yaml:"requests_per_minute,omitempty"`

	// The burst of request packet size token bucket for user
	// if omitted or zero - no limits would be applied
	ReqPacketSizeTokensBurst ByteSize `yaml:"request_packet_size_tokens_burst,omitempty"`

	// The request packet size tokens produced rate per second for user
	// if omitted or zero - no limits would be applied
	ReqPacketSizeTokensRate ByteSize `yaml:"request_packet_size_tokens_rate,omitempty"`

	// Maximum number of queries waiting for execution in the queue
	// if omitted or zero - queries are executed without waiting
	// in the queue
	MaxQueueSize uint32 `yaml:"max_queue_size,omitempty"`

	// Maximum duration the query may wait in the queue
	// if omitted or zero - 10s duration is used
	MaxQueueTime Duration `yaml:"max_queue_time,omitempty"`

	NetworksOrGroups NetworksOrGroups `yaml:"allowed_networks,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"-"`

	// Whether to deny http connections for this user
	DenyHTTP bool `yaml:"deny_http,omitempty"`

	// Whether to deny https connections for this user
	DenyHTTPS bool `yaml:"deny_https,omitempty"`

	// Whether to allow CORS requests for this user
	AllowCORS bool `yaml:"allow_cors,omitempty"`

	// Name of Cache configuration to use for responses of this user
	Cache string `yaml:"cache,omitempty"`

	// Name of ParamGroup to use
	Params string `yaml:"params,omitempty"`

	// prefix_*
	IsWildcarded bool `yaml:"is_wildcarded,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (u *User) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain User
	if err := unmarshal((*plain)(u)); err != nil {
		return err
	}

	if err := u.validate(); err != nil {
		return err
	}

	return checkOverflow(u.XXX, fmt.Sprintf("user %q", u.Name))
}

func (u *User) validate() error {
	if len(u.Name) == 0 {
		return fmt.Errorf("`user.name` cannot be empty")
	}

	if len(u.ToUser) == 0 {
		return fmt.Errorf("`user.to_user` cannot be empty for %q", u.Name)
	}

	if len(u.ToCluster) == 0 {
		return fmt.Errorf("`user.to_cluster` cannot be empty for %q", u.Name)
	}

	if u.DenyHTTP && u.DenyHTTPS {
		return fmt.Errorf("`deny_http` and `deny_https` cannot be simultaneously set to `true` for %q", u.Name)
	}

	if err := u.validateWildcarded(); err != nil {
		return err
	}

	if err := u.validateRateLimitConfig(); err != nil {
		return err
	}

	return nil
}

func (u *User) validateWildcarded() error {
	if u.IsWildcarded {
		if s := strings.Split(u.Name, "*"); !(len(s) == 2 && (s[0] == "" || s[1] == "")) {
			return fmt.Errorf("user name %q marked 'is_wildcared' does not match 'prefix*' or '*suffix' or '*'", u.Name)
		}
	}

	return nil
}

func (u *User) validateRateLimitConfig() error {
	if u.MaxQueueTime > 0 && u.MaxQueueSize == 0 {
		return fmt.Errorf("`max_queue_size` must be set if `max_queue_time` is set for %q", u.Name)
	}

	if u.ReqPacketSizeTokensBurst > 0 && u.ReqPacketSizeTokensRate == 0 {
		return fmt.Errorf("`request_packet_size_tokens_rate` must be set if `request_packet_size_tokens_burst` is set for %q", u.Name)
	}

	return nil
}

func (u *User) validateSecurity(hasHTTP, hasHTTPS bool) error {
	if len(u.Password) == 0 {
		if !u.DenyHTTPS && hasHTTPS {
			return fmt.Errorf("https: user %q has neither password nor `allowed_networks` on `user` or `server.http` level",
				u.Name)
		}
		if !u.DenyHTTP && hasHTTP {
			return fmt.Errorf("http: user %q has neither password nor `allowed_networks` on `user` or `server.http` level",
				u.Name)
		}
	}
	if len(u.Password) > 0 && hasHTTP {
		return fmt.Errorf("http: user %q is allowed to connect via http, but not limited by `allowed_networks` "+
			"on `user` or `server.http` level - password could be stolen", u.Name)
	}

	return nil
}

func (u *User) setDefaults() {
	if u.MaxExecutionTime == 0 {
		u.MaxExecutionTime = defaultExecutionTime
	}
}

// NetworkGroups describes a named Networks lists
type NetworkGroups struct {
	// Name of the group
	Name string `yaml:"name"`

	// List of networks
	// Each list item could be IP address or subnet mask
	Networks Networks `yaml:"networks"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (ng *NetworkGroups) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain NetworkGroups
	if err := unmarshal((*plain)(ng)); err != nil {
		return err
	}
	if len(ng.Name) == 0 {
		return fmt.Errorf("`network_group.name` must be specified")
	}
	if len(ng.Networks) == 0 {
		return fmt.Errorf("`network_group.networks` must contain at least one network")
	}
	return checkOverflow(ng.XXX, fmt.Sprintf("network_group %q", ng.Name))
}

// NetworksOrGroups is a list of strings with names of NetworkGroups
// or just Networks
type NetworksOrGroups []string

// Cache describes configuration options for caching
// responses from CH clusters
type Cache struct {
	// Mode of cache (file_system, redis)
	// todo make it an enum
	Mode string `yaml:"mode"`

	// Name of configuration for further assign
	Name string `yaml:"name"`

	// Expiration period for cached response
	// Files which are older than expiration period will be deleted
	// on new request and re-cached
	Expire Duration `yaml:"expire,omitempty"`

	// Deprecated: GraceTime duration before the expired entry is deleted from the cache.
	// It's deprecated and in future versions it'll be replaced by user's MaxExecutionTime.
	// It's already the case today if value of GraceTime is omitted.
	GraceTime Duration `yaml:"grace_time,omitempty"`

	FileSystem FileSystemCacheConfig `yaml:"file_system,omitempty"`

	Redis RedisCacheConfig `yaml:"redis,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`

	// Maximum total size of request payload for caching
	MaxPayloadSize ByteSize `yaml:"max_payload_size,omitempty"`

	// Whether a query cached by a user could be used by another user
	SharedWithAllUsers bool `yaml:"shared_with_all_users,omitempty"`
}

func (c *Cache) setDefaults() {
	if c.MaxPayloadSize <= 0 {
		c.MaxPayloadSize = defaultMaxPayloadSize
	}
}

type FileSystemCacheConfig struct {
	//// Path to directory where cached files will be saved
	Dir string `yaml:"dir"`

	// Maximum total size of all cached to Dir files
	// If size is exceeded - the oldest files in Dir will be deleted
	// until total size becomes normal
	MaxSize ByteSize `yaml:"max_size"`
}

type RedisCacheConfig struct {
	TLS       `yaml:",inline"`
	EnableTLS bool `yaml:"enable_tls,omitempty"`

	Username  string                 `yaml:"username,omitempty"`
	Password  string                 `yaml:"password,omitempty"`
	Addresses []string               `yaml:"addresses"`
	DBIndex   int                    `yaml:"db_index,omitempty"`
	PoolSize  int                    `yaml:"pool_size,omitempty"`
	XXX       map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Cache) UnmarshalYAML(unmarshal func(interface{}) error) (err error) {
	type plain Cache
	if err = unmarshal((*plain)(c)); err != nil {
		return err
	}
	if len(c.Name) == 0 {
		return fmt.Errorf("`cache.name` must be specified")
	}

	switch c.Mode {
	case "file_system":
		err = c.checkFileSystemConfig()
	case "redis":
		err = c.checkRedisConfig()
	default:
		err = fmt.Errorf("not supported cache type %v. Supported types: [file_system]", c.Mode)
	}

	if err != nil {
		return fmt.Errorf("failed to configure cache for %q", c.Name)
	}

	return checkOverflow(c.XXX, fmt.Sprintf("cache %q", c.Name))
}

func (c *Cache) checkFileSystemConfig() error {
	if len(c.FileSystem.Dir) == 0 {
		return fmt.Errorf("`cache.filesystem.dir` must be specified for %q", c.Name)
	}
	if c.FileSystem.MaxSize <= 0 {
		return fmt.Errorf("`cache.filesystem.max_size` must be specified for %q", c.Name)
	}
	return nil
}

func (c *Cache) checkRedisConfig() error {
	if len(c.Redis.Addresses) == 0 {
		return fmt.Errorf("`cache.redis.addresses` must be specified for %q", c.Name)
	}
	return nil
}

// ParamGroup describes named group of GET params
// for sending with each query
type ParamGroup struct {
	// Name of configuration for further assign
	Name string `yaml:"name"`

	// Params contains a list of GET params
	Params []Param `yaml:"params"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (pg *ParamGroup) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ParamGroup
	if err := unmarshal((*plain)(pg)); err != nil {
		return err
	}
	if len(pg.Name) == 0 {
		return fmt.Errorf("`param_group.name` must be specified")
	}
	if len(pg.Params) == 0 {
		return fmt.Errorf("`param_group.params` must contain at least one param")
	}
	return checkOverflow(pg.XXX, fmt.Sprintf("param_group %q", pg.Name))
}

// Param describes URL param value
type Param struct {
	// Key is a name of params
	Key string `yaml:"key"`
	// Value is a value of param
	Value string `yaml:"value"`
}

// ConnectionPool describes pool of connection with ClickHouse
// settings
type ConnectionPool struct {
	// Maximum total number of idle connections between chproxy and all ClickHouse instances
	MaxIdleConns int `yaml:"max_idle_conns,omitempty"`

	// Maximum number of idle connections between chproxy and particuler ClickHouse instance
	MaxIdleConnsPerHost int `yaml:"max_idle_conns_per_host,omitempty"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cp *ConnectionPool) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*cp = defaultConnectionPool
	type plain ConnectionPool
	if err := unmarshal((*plain)(cp)); err != nil {
		return err
	}
	if cp.MaxIdleConnsPerHost > cp.MaxIdleConns || cp.MaxIdleConns < 0 {
		return fmt.Errorf("inconsistent ConnectionPool settings")
	}
	return checkOverflow(cp.XXX, "connection_pool")
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

	// Maximum duration of query execution for user
	// if omitted or zero - limit is set to 120 seconds
	MaxExecutionTime Duration `yaml:"max_execution_time,omitempty"`

	// Maximum number of requests per minute for user
	// if omitted or zero - no limits would be applied
	// if negative - the user is effectively blocked
	ReqPerMin int32 `yaml:"requests_per_minute,omitempty"`

	// The burst of request packet size token bucket for user
	// if omitted or zero - no limits would be applied
	ReqPacketSizeTokensBurst ByteSize `yaml:"request_packet_size_tokens_burst,omitempty"`

	// The request packet size tokens produced rate for user
	// if omitted or zero - no limits would be applied
	ReqPacketSizeTokensRate ByteSize `yaml:"request_packet_size_tokens_rate,omitempty"`

	// Maximum number of queries waiting for execution in the queue
	// if omitted or zero - queries are executed without waiting
	// in the queue
	MaxQueueSize uint32 `yaml:"max_queue_size,omitempty"`

	// Maximum duration the query may wait in the queue
	// if omitted or zero - 10s duration is used
	MaxQueueTime Duration `yaml:"max_queue_time,omitempty"`

	NetworksOrGroups NetworksOrGroups `yaml:"allowed_networks,omitempty"`

	// List of networks that access is allowed from
	// Each list item could be IP address or subnet mask
	// if omitted or zero - no limits would be applied
	AllowedNetworks Networks `yaml:"-"`

	// Catches all undefined fields
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cu *ClusterUser) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ClusterUser
	if err := unmarshal((*plain)(cu)); err != nil {
		return err
	}

	if len(cu.Name) == 0 {
		return fmt.Errorf("`cluster.user.name` cannot be empty")
	}

	if cu.MaxQueueTime > 0 && cu.MaxQueueSize == 0 {
		return fmt.Errorf("`max_queue_size` must be set if `max_queue_time` is set for %q", cu.Name)
	}

	if cu.ReqPacketSizeTokensBurst > 0 && cu.ReqPacketSizeTokensRate == 0 {
		return fmt.Errorf("`request_packet_size_tokens_rate` must be set if `request_packet_size_tokens_burst` is set for %q", cu.Name)
	}

	return checkOverflow(cu.XXX, fmt.Sprintf("cluster.user %q", cu.Name))
}

// LoadFile loads and validates configuration from provided .yml file
func LoadFile(filename string) (*Config, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	content = findAndReplacePlaceholders(content)

	cfg := &Config{}
	if err := yaml.Unmarshal(content, cfg); err != nil {
		return nil, err
	}
	cfg.networkReg = make(map[string]Networks, len(cfg.NetworkGroups))
	for _, ng := range cfg.NetworkGroups {
		if _, ok := cfg.networkReg[ng.Name]; ok {
			return nil, fmt.Errorf("duplicate `network_groups.name` %q", ng.Name)
		}
		cfg.networkReg[ng.Name] = ng.Networks
	}
	if cfg.Server.HTTP.AllowedNetworks, err = cfg.groupToNetwork(cfg.Server.HTTP.NetworksOrGroups); err != nil {
		return nil, err
	}
	if cfg.Server.HTTPS.AllowedNetworks, err = cfg.groupToNetwork(cfg.Server.HTTPS.NetworksOrGroups); err != nil {
		return nil, err
	}
	if cfg.Server.Metrics.AllowedNetworks, err = cfg.groupToNetwork(cfg.Server.Metrics.NetworksOrGroups); err != nil {
		return nil, err
	}

	if err := cfg.setDefaults(); err != nil {
		return nil, err
	}

	if err := cfg.checkVulnerabilities(); err != nil {
		return nil, fmt.Errorf("security breach: %w\nSet option `hack_me_please=true` to disable security errors", err)
	}
	return cfg, nil
}

var envVarRegex = regexp.MustCompile(`\${([a-zA-Z_][a-zA-Z0-9_]*)}`)

// findAndReplacePlaceholders finds all environment variables placeholders in the config.
// Each placeholder is a string like ${VAR_NAME}. They will be replaced with the value of the
// corresponding environment variable. It returns the new content with replaced placeholders.
func findAndReplacePlaceholders(content []byte) []byte {
	for _, match := range envVarRegex.FindAllSubmatch(content, -1) {
		envVar := os.Getenv(string(match[1]))
		if envVar != "" {
			content = bytes.ReplaceAll(content, match[0], []byte(envVar))
		}
	}

	return content
}

func (c Config) checkVulnerabilities() error {
	if c.HackMePlease {
		return nil
	}

	hasHTTPS := len(c.Server.HTTPS.ListenAddr) > 0 && len(c.Server.HTTPS.NetworksOrGroups) == 0
	hasHTTP := len(c.Server.HTTP.ListenAddr) > 0 && len(c.Server.HTTP.NetworksOrGroups) == 0
	for _, u := range c.Users {
		if len(u.NetworksOrGroups) != 0 {
			continue
		}
		if err := u.validateSecurity(hasHTTP, hasHTTPS); err != nil {
			return err
		}
	}
	return nil
}

type LogMask struct {
	Regex       string `yaml:"regex"`
	Replacement string `yaml:"replacement"`
}
