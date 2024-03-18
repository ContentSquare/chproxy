package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/global/types"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/mohae/deepcopy"
	"gopkg.in/yaml.v2"
)

var redisPort = types.RedisPort

var fullConfig = Config{
	Caches: []Cache{
		{
			Name: "longterm",
			Mode: "file_system",
			FileSystem: FileSystemCacheConfig{
				Dir:     "/path/to/longterm/cachedir",
				MaxSize: ByteSize(100 << 30),
			},
			Expire:             Duration(time.Hour),
			GraceTime:          Duration(20 * time.Second),
			MaxPayloadSize:     ByteSize(100 << 30),
			SharedWithAllUsers: false,
		},
		{
			Name: "shortterm",
			Mode: "file_system",
			FileSystem: FileSystemCacheConfig{
				Dir:     "/path/to/shortterm/cachedir",
				MaxSize: ByteSize(100 << 20),
			},
			Expire:             Duration(10 * time.Second),
			MaxPayloadSize:     ByteSize(100 << 20),
			SharedWithAllUsers: true,
		},
		{
			Name:               "redis-cache",
			Mode:               "redis",
			Expire:             Duration(10 * time.Second),
			MaxPayloadSize:     ByteSize(100 << 30),
			SharedWithAllUsers: true,
			Redis: RedisCacheConfig{
				Username:  "chproxy",
				Password:  "password",
				Addresses: []string{"127.0.0.1:" + redisPort},
				PoolSize:  10,
			},
		},
	},
	HackMePlease: true,
	Server: Server{
		HTTP: HTTP{
			ListenAddr:           ":9090",
			NetworksOrGroups:     []string{"office", "reporting-apps", "1.2.3.4"},
			ForceAutocertHandler: true,
			TimeoutCfg: TimeoutCfg{
				ReadTimeout:  Duration(5 * time.Minute),
				WriteTimeout: Duration(10 * time.Minute),
				IdleTimeout:  Duration(20 * time.Minute),
			},
		},
		HTTPS: HTTPS{
			ListenAddr: ":443",
			TLS: TLS{
				Autocert: Autocert{
					CacheDir:     "certs_dir",
					AllowedHosts: []string{"example.com"},
				},
			},
			TimeoutCfg: TimeoutCfg{
				ReadTimeout:  Duration(time.Minute),
				WriteTimeout: Duration(215 * time.Second),
				IdleTimeout:  Duration(10 * time.Minute),
			},
		},
		Metrics: Metrics{
			NetworksOrGroups: []string{"office"},
		},
		Proxy: Proxy{
			Enable: true,
			Header: "CF-Connecting-IP",
		},
	},
	LogDebug: true,

	Clusters: []Cluster{
		{
			Name:   "first cluster",
			Scheme: "http",
			Nodes:  []string{"127.0.0.1:8123", "shard2:8123"},
			KillQueryUser: KillQueryUser{
				Name:     "default",
				Password: "***",
			},
			ClusterUsers: []ClusterUser{
				{
					Name:                 "web",
					Password:             "password",
					MaxConcurrentQueries: 4,
					MaxExecutionTime:     Duration(time.Minute),
				},
			},
			RetryNumber: 1,
			HeartBeat: HeartBeat{
				Interval: Duration(5 * time.Second),
				Timeout:  Duration(3 * time.Second),
				Request:  "/ping",
				Response: "Ok.\n",
			},
		},
		{
			Name:   "second cluster",
			Scheme: "https",
			Replicas: []Replica{
				{
					Name:  "replica1",
					Nodes: []string{"127.0.1.1:8443", "127.0.1.2:8443"},
				},
				{
					Name:  "replica2",
					Nodes: []string{"127.0.2.1:8443", "127.0.2.2:8443"},
				},
			},
			ClusterUsers: []ClusterUser{
				{
					Name:                 "default",
					MaxConcurrentQueries: 4,
					MaxExecutionTime:     Duration(time.Minute),
				},
				{
					Name:                 "web",
					ReqPerMin:            10,
					MaxConcurrentQueries: 4,
					MaxExecutionTime:     Duration(10 * time.Second),
					NetworksOrGroups:     []string{"office"},
					MaxQueueSize:         50,
					MaxQueueTime:         Duration(70 * time.Second),
				},
			},
			RetryNumber: 2,
			HeartBeat: HeartBeat{
				Interval: Duration(5 * time.Second),
				Timeout:  Duration(3 * time.Second),
				Request:  "/ping",
				Response: "Ok.\n",
				User:     "hbuser",
				Password: "hbpassword",
			},
		},
		{
			Name:   "third cluster",
			Scheme: "http",
			Nodes:  []string{"third1:8123", "third2:8123"},
			ClusterUsers: []ClusterUser{
				{
					Name: "default",
				},
			},
			RetryNumber: 3,
			HeartBeat: HeartBeat{
				Interval: Duration(2 * time.Minute),
				Timeout:  Duration(10 * time.Second),
				Request:  "/?query=SELECT%201",
				Response: "Ok.\n",
			},
		},
	},

	ParamGroups: []ParamGroup{
		{
			Name: "cron-job",
			Params: []Param{
				{
					Key:   "max_memory_usage",
					Value: "40000000000",
				},
				{
					Key:   "max_bytes_before_external_group_by",
					Value: "20000000000",
				},
			},
		},
		{
			Name: "web",
			Params: []Param{
				{
					Key:   "max_memory_usage",
					Value: "5000000000",
				},
				{
					Key:   "max_columns_to_read",
					Value: "30",
				},
				{
					Key:   "max_execution_time",
					Value: "30",
				},
			},
		},
	},

	ConnectionPool: ConnectionPool{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 2,
	},

	Users: []User{
		{
			Name:             "web",
			Password:         "****",
			ToCluster:        "first cluster",
			ToUser:           "web",
			DenyHTTP:         true,
			AllowCORS:        true,
			ReqPerMin:        4,
			MaxQueueSize:     100,
			MaxQueueTime:     Duration(35 * time.Second),
			MaxExecutionTime: Duration(2 * time.Minute),
			Cache:            "longterm",
			Params:           "web",
		},
		{
			Name:                 "default",
			ToCluster:            "second cluster",
			ToUser:               "default",
			MaxConcurrentQueries: 4,
			MaxExecutionTime:     Duration(time.Minute),
			DenyHTTPS:            true,
			NetworksOrGroups:     []string{"office", "1.2.3.0/24"},
		},
	},
	NetworkGroups: []NetworkGroups{
		{
			Name: "office",
			Networks: Networks{
				&net.IPNet{
					IP:   net.IPv4(127, 0, 0, 0),
					Mask: net.IPMask{255, 255, 255, 0},
				},
				&net.IPNet{
					IP:   net.IPv4(10, 10, 0, 1),
					Mask: net.IPMask{255, 255, 255, 255},
				},
			},
		},
		{
			Name: "reporting-apps",
			Networks: Networks{
				&net.IPNet{
					IP:   net.IPv4(10, 10, 10, 0),
					Mask: net.IPMask{255, 255, 255, 0},
				},
			},
		},
	},
	MaxErrorReasonSize: ByteSize(100 << 20),
	networkReg:         map[string]Networks{},
}

func TestLoadConfig(t *testing.T) {
	var testCases = []struct {
		name string
		file string
		cfg  Config
	}{
		{
			"full description",
			"testdata/full.yml",

			fullConfig,
		},
		{
			"default values",
			"testdata/default_values.yml",
			Config{
				Server: Server{
					HTTP: HTTP{
						ListenAddr:       ":8080",
						NetworksOrGroups: []string{"127.0.0.1"},
						TimeoutCfg: TimeoutCfg{
							ReadTimeout:  Duration(time.Minute),
							WriteTimeout: Duration(3 * time.Minute),
							IdleTimeout:  Duration(10 * time.Minute),
						},
					},
				},
				Clusters: []Cluster{
					{
						Name:   "cluster",
						Scheme: "http",
						Nodes:  []string{"127.0.0.1:8123"},
						ClusterUsers: []ClusterUser{
							{
								Name: "default",
							},
						},
						HeartBeat: HeartBeat{
							Interval: Duration(5 * time.Second),
							Timeout:  Duration(3 * time.Second),
							Request:  "/ping",
							Response: "Ok.\n",
						},
						RetryNumber: 0,
					},
				},
				Users: []User{
					{
						Name:             "default",
						ToCluster:        "cluster",
						ToUser:           "default",
						MaxExecutionTime: Duration(120 * time.Second),
					},
				},
				MaxErrorReasonSize: ByteSize(1 << 50),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := LoadFile(tc.file)
			if err != nil {
				t.Fatalf("Error parsing %s: %s", tc.file, err)
			}
			got, err := yaml.Marshal(c)
			if err != nil {
				t.Fatalf("%s", err)
			}
			exp, err := yaml.Marshal(&tc.cfg)
			if err != nil {
				t.Fatalf("%s", err)
			}
			if !bytes.Equal(got, exp) {
				t.Fatalf("unexpected config result. Diff: %s", cmp.Diff(got, exp))
			}
		})
	}
}

func TestBadConfig(t *testing.T) {
	var testCases = []struct {
		name  string
		file  string
		error string
	}{
		{
			"no file",
			"testdata/nofile.yml",
			"open testdata/nofile.yml: no such file or directory",
		},
		{
			"extra fields",
			"testdata/bad.extra_fields.yml",
			"unknown fields in cluster \"second cluster\": unknown_field",
		},
		{
			"empty users",
			"testdata/bad.empty_users.yml",
			"`users` must contain at least 1 user",
		},
		{
			"empty nodes",
			"testdata/bad.empty_nodes.yml",
			"either `cluster.nodes` or `cluster.replicas` must be set for \"second cluster\"",
		},
		{
			"empty replica nodes",
			"testdata/bad.empty_replica_nodes.yml",
			"`replica.nodes` cannot be empty for \"bar\"",
		},
		{
			"nodes and replicas",
			"testdata/bad.nodes_and_replicas.yml",
			"`cluster.nodes` cannot be simultaneously set with `cluster.replicas` for \"second cluster\"",
		},
		{
			"wrong scheme",
			"testdata/bad.wrong_scheme.yml",
			"`cluster.scheme` must be `http` or `https`, got \"tcp\" instead for \"second cluster\"",
		},
		{
			"empty https",
			"testdata/bad.empty_https.yml",
			"configuration `https` is missing. Must be specified `https.cache_dir` for autocert OR `https.key_file` and `https.cert_file` for already existing certs",
		},
		{
			"empty https cert key",
			"testdata/bad.empty_https_key_file.yml",
			"`https.key_file` must be specified",
		},
		{
			"double certification",
			"testdata/bad.double_certification.yml",
			"it is forbidden to specify certificate and `https.autocert` at the same time. Choose one way",
		},
		{
			"security no password",
			"testdata/bad.security_no_pass.yml",
			"security breach: https: user \"dummy\" has neither password nor `allowed_networks` on `user` or `server.http` level" +
				"\nSet option `hack_me_please=true` to disable security errors",
		},
		{
			"security no allowed networks",
			"testdata/bad.security_no_an.yml",
			"security breach: http: user \"dummy\" is allowed to connect via http, but not limited by `allowed_networks` " +
				"on `user` or `server.http` level - password could be stolen" +
				"\nSet option `hack_me_please=true` to disable security errors",
		},
		{
			"allow all",
			"testdata/bad.allow_all.yml",
			"suspicious mask specified \"0.0.0.0/0\". " +
				"If you want to allow all then just omit `allowed_networks` field",
		},
		{
			"deny all",
			"testdata/bad.deny_all.yml",
			"`deny_http` and `deny_https` cannot be simultaneously set to `true` for \"dummy\"",
		},
		{
			"autocert allowed networks",
			"testdata/bad.autocert_an.yml",
			"`letsencrypt` specification requires https server to be without `allowed_networks` limits. " +
				"Otherwise, certificates will be impossible to generate",
		},
		{
			"incorrect network group name",
			"testdata/bad.network_groups.yml",
			"wrong network group name or address \"office\": invalid CIDR address: office/32",
		},
		{
			"empty network group name",
			"testdata/bad.network_groups.name.yml",
			"`network_group.name` must be specified",
		},
		{
			"empty network group networks",
			"testdata/bad.network_groups.networks.yml",
			"`network_group.networks` must contain at least one network",
		},
		{
			"double network group",
			"testdata/bad.double_network_groups.yml",
			"duplicate `network_groups.name` \"office\"",
		},
		{
			"max queue size and time on user",
			"testdata/bad.queue_size_time_user.yml",
			"`max_queue_size` must be set if `max_queue_time` is set for \"default\"",
		},
		{
			"max queue size and time on cluster_user",
			"testdata/bad.queue_size_time_cluster_user.yml",
			"`max_queue_size` must be set if `max_queue_time` is set for \"default\"",
		},
		{
			"packet size token burst and rate on user",
			"testdata/bad.packet_size_token_burst_rate_user.yml",
			"`request_packet_size_tokens_rate` must be set if `request_packet_size_tokens_burst` is set for \"default\"",
		},
		{
			"packet size token burst and rate on user on cluster_user",
			"testdata/bad.packet_size_token_burst_rate_cluster_user.yml",
			"`request_packet_size_tokens_rate` must be set if `request_packet_size_tokens_burst` is set for \"default\"",
		},
		{
			"cache max size",
			"testdata/bad.cache_max_size.yml",
			"cannot parse byte size \"-10B\": it must be positive float followed by optional units. For example, 1.5Gb, 3T",
		},
		{
			"empty param group name",
			"testdata/bad.param_groups.name.yml",
			"`param_group.name` must be specified",
		},
		{
			"empty param group params",
			"testdata/bad.param_groups.params.yml",
			"`param_group.params` must contain at least one param",
		},
		{
			"empty heartbeat section",
			"testdata/bad.heartbeat_section.empty.yml",
			"`cluster.heartbeat` cannot be unset for \"cluster\"",
		},
		{
			"max payload size to cache",
			"testdata/bad.max_payload_size.yml",
			"cannot parse byte size \"-10B\": it must be positive float followed by optional units. For example, 1.5Gb, 3T",
		},
		{
			"user is marked as is_wildcarded, but it's name is not consist of a prefix, underscore and asterisk",
			"testdata/bad.wildcarded_users.no.wildcard.yml",
			"user name \"analyst_named\" marked 'is_wildcared' does not match 'prefix*' or '*suffix' or '*'",
		},
		{
			"proxy header without enabling proxy settings",
			"testdata/bad.proxy_settings.yml",
			"`proxy_header` cannot be set without enabling proxy settings",
		},
		{
			"max error reason size",
			"testdata/bad.max_error_reason_size.yml",
			"cannot parse byte size \"-10B\": it must be positive float followed by optional units. For example, 1.5Gb, 3T",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFile(tc.file)
			if err == nil {
				t.Fatalf("error expected")
			}
			if err.Error() != tc.error {
				t.Fatalf("expected: %q; got: %q", tc.error, err)
			}
		})
	}
}

func TestExamples(t *testing.T) {
	var testCases = []struct {
		name string
		file string
	}{
		{
			"simple",
			"examples/simple.yml",
		},
		{
			"spread inserts",
			"examples/spread.inserts.yml",
		},
		{
			"spread selects",
			"examples/spread.selects.yml",
		},
		{
			"https",
			"examples/https.yml",
		},
		{
			"combined",
			"examples/combined.yml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFile(tc.file)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	var testCases = []struct {
		value    string
		expected time.Duration
	}{
		{
			"10ns",
			time.Duration(10),
		},
		{
			"20µs",
			20 * time.Microsecond,
		},
		{
			"30ms",
			30 * time.Millisecond,
		},
		{
			"40s",
			40 * time.Second,
		},
		{
			"50m",
			50 * time.Minute,
		},
		{
			"60h",
			60 * time.Hour,
		},
		{
			"75d",
			75 * 24 * time.Hour,
		},
		{
			"80w",
			80 * 7 * 24 * time.Hour,
		},
	}
	for _, tc := range testCases {
		v, err := StringToDuration(tc.value)
		if err != nil {
			t.Fatalf("unexpected duration conversion error: %s", err)
		}
		got := time.Duration(v)
		if got != tc.expected {
			t.Fatalf("unexpected value - got: %v; expected: %v", got, tc.expected)
		}
		if v.String() != tc.value {
			t.Fatalf("unexpected toString conversion - got: %q; expected: %q", v, tc.value)
		}
	}
}

func TestParseDurationNegative(t *testing.T) {
	var testCases = []struct {
		value, error string
	}{
		{
			"10",
			"not a valid duration string: \"10\"",
		},
		{
			"20ks",
			"not a valid duration string: \"20ks\"",
		},
		{
			"30Ms",
			"not a valid duration string: \"30Ms\"",
		},
		{
			"40 ms",
			"not a valid duration string: \"40 ms\"",
		},
		{
			"50y",
			"not a valid duration string: \"50y\"",
		},
		{
			"1.5h",
			"not a valid duration string: \"1.5h\"",
		},
	}
	for _, tc := range testCases {
		_, err := StringToDuration(tc.value)
		if err == nil {
			t.Fatalf("expected to get parse error; got: nil")
		}
		if err.Error() != tc.error {
			t.Fatalf("unexpected error - got: %q; expected: %q", err, tc.error)
		}
	}
}

func TestConfigTimeouts(t *testing.T) {
	var testCases = []struct {
		name        string
		file        string
		expectedCfg TimeoutCfg
	}{
		{
			"default",
			"testdata/default_values.yml",
			TimeoutCfg{
				ReadTimeout:  Duration(time.Minute),
				WriteTimeout: Duration(3 * time.Minute), // defaultExecutionTime + 1 min
				IdleTimeout:  Duration(10 * time.Minute),
			},
		},
		{
			"defined",
			"testdata/timeouts.defined.yml",
			TimeoutCfg{
				ReadTimeout:  Duration(time.Minute),
				WriteTimeout: Duration(time.Hour),
				IdleTimeout:  Duration(24 * time.Hour),
			},
		},
		{
			"calculated write 1",
			"testdata/timeouts.write.calculated.yml",
			TimeoutCfg{
				ReadTimeout: Duration(time.Minute),
				// 10 + 1 minute
				WriteTimeout: Duration(11 * 60 * time.Second),
				IdleTimeout:  Duration(10 * time.Minute),
			},
		},
		{
			"calculated write 2",
			"testdata/timeouts.write.calculated2.yml",
			TimeoutCfg{
				ReadTimeout: Duration(time.Minute),
				// 20 + 1 minute
				WriteTimeout: Duration(21 * 60 * time.Second),
				IdleTimeout:  Duration(10 * time.Minute),
			},
		},
		{
			"calculated write 3",
			"testdata/timeouts.write.calculated3.yml",
			TimeoutCfg{
				ReadTimeout: Duration(time.Minute),
				// 50 + 1 minute
				WriteTimeout: Duration(51 * 60 * time.Second),
				IdleTimeout:  Duration(10 * time.Minute),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadFile(tc.file)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			got := cfg.Server.HTTP.TimeoutCfg
			if got.ReadTimeout != tc.expectedCfg.ReadTimeout {
				t.Fatalf("got ReadTimeout %v; expected to have: %v", got.ReadTimeout, tc.expectedCfg.ReadTimeout)
			}
			if got.WriteTimeout != tc.expectedCfg.WriteTimeout {
				t.Fatalf("got WriteTimeout %v; expected to have: %v", got.WriteTimeout, tc.expectedCfg.WriteTimeout)
			}
			if got.IdleTimeout != tc.expectedCfg.IdleTimeout {
				t.Fatalf("got IdleTimeout %v; expected to have: %v", got.IdleTimeout, tc.expectedCfg.IdleTimeout)
			}
		})
	}
}

func TestRemovalSensitiveData(t *testing.T) {
	conf := deepcopy.Copy(&fullConfig).(*Config)
	confSafe := withoutSensitiveInfo(conf)

	if cmp.Equal(conf, confSafe, cmpopts.IgnoreUnexported(Config{})) {
		t.Fatalf("confCopy should have sensitive data replaced with XXX values")
	}
	// We're obfuscating all the passwords the way withoutSensitiveInfo() is supposed to do
	conf.Users[0].Password = "XXX"
	conf.Users[1].Password = "XXX"
	conf.Clusters[0].ClusterUsers[0].Password = "XXX"
	conf.Clusters[0].KillQueryUser.Password = "XXX"
	conf.Clusters[1].ClusterUsers[0].Password = "XXX"
	conf.Clusters[1].ClusterUsers[1].Password = "XXX"
	conf.Clusters[2].ClusterUsers[0].Password = "XXX"
	conf.Caches[2].Redis.Password = "XXX"

	if !cmp.Equal(conf, confSafe, cmpopts.IgnoreUnexported(Config{})) {
		t.Fatalf("confCopy should have sensitive data replaced with XXX values,\n the diff is: %s",
			cmp.Diff(conf, confSafe, cmpopts.IgnoreUnexported(Config{})))

	}
}

func TestConfigString(t *testing.T) {
	expected := fmt.Sprintf(`server:
  http:
    listen_addr: :9090
    allowed_networks:
    - office
    - reporting-apps
    - 1.2.3.4
    forceautocerthandler: true
    read_timeout: 5m
    write_timeout: 10m
    idle_timeout: 20m
  https:
    listen_addr: :443
    autocert:
      cache_dir: certs_dir
      allowed_hosts:
      - example.com
    read_timeout: 1m
    write_timeout: 215s
    idle_timeout: 10m
  metrics:
    allowed_networks:
    - office
  proxy:
    enable: true
    header: CF-Connecting-IP
clusters:
- name: first cluster
  scheme: http
  nodes:
  - 127.0.0.1:8123
  - shard2:8123
  users:
  - name: web
    password: XXX
    max_concurrent_queries: 4
    max_execution_time: 1m
  kill_query_user:
    name: default
    password: XXX
  heartbeat:
    interval: 5s
    timeout: 3s
    request: /ping
    response: |
      Ok.
  retry_number: 1
- name: second cluster
  scheme: https
  replicas:
  - name: replica1
    nodes:
    - 127.0.1.1:8443
    - 127.0.1.2:8443
  - name: replica2
    nodes:
    - 127.0.2.1:8443
    - 127.0.2.2:8443
  users:
  - name: default
    password: XXX
    max_concurrent_queries: 4
    max_execution_time: 1m
  - name: web
    password: XXX
    max_concurrent_queries: 4
    max_execution_time: 10s
    requests_per_minute: 10
    max_queue_size: 50
    max_queue_time: 70s
    allowed_networks:
    - office
  heartbeat:
    interval: 5s
    timeout: 3s
    request: /ping
    response: |
      Ok.
    user: hbuser
    password: hbpassword
  retry_number: 2
- name: third cluster
  scheme: http
  nodes:
  - third1:8123
  - third2:8123
  users:
  - name: default
    password: XXX
  heartbeat:
    interval: 2m
    timeout: 10s
    request: /?query=SELECT%%201
    response: |
      Ok.
  retry_number: 3
users:
- name: web
  password: XXX
  to_cluster: first cluster
  to_user: web
  max_execution_time: 2m
  requests_per_minute: 4
  max_queue_size: 100
  max_queue_time: 35s
  deny_http: true
  allow_cors: true
  cache: longterm
  params: web
- name: default
  password: XXX
  to_cluster: second cluster
  to_user: default
  max_concurrent_queries: 4
  max_execution_time: 1m
  allowed_networks:
  - office
  - 1.2.3.0/24
  deny_https: true
log_debug: true
hack_me_please: true
network_groups:
- name: office
  networks:
  - 127.0.0.0/24
  - 10.10.0.1/32
- name: reporting-apps
  networks:
  - 10.10.10.0/24
max_error_reason_size: 104857600
caches:
- mode: file_system
  name: longterm
  expire: 1h
  grace_time: 20s
  file_system:
    dir: /path/to/longterm/cachedir
    max_size: 107374182400
  max_payload_size: 107374182400
- mode: file_system
  name: shortterm
  expire: 10s
  file_system:
    dir: /path/to/shortterm/cachedir
    max_size: 104857600
  max_payload_size: 104857600
  shared_with_all_users: true
- mode: redis
  name: redis-cache
  expire: 10s
  redis:
    username: chproxy
    password: XXX
    addresses:
    - 127.0.0.1:%s
    pool_size: 10
  max_payload_size: 107374182400
  shared_with_all_users: true
param_groups:
- name: cron-job
  params:
  - key: max_memory_usage
    value: "40000000000"
  - key: max_bytes_before_external_group_by
    value: "20000000000"
- name: web
  params:
  - key: max_memory_usage
    value: "5000000000"
  - key: max_columns_to_read
    value: "30"
  - key: max_execution_time
    value: "30"
connection_pool:
  max_idle_conns: 100
  max_idle_conns_per_host: 2
`, redisPort)
	tested := fullConfig.String()
	if tested != expected {
		t.Fatalf("the stringify version of fullConfig is not what it's expected: %s",
			cmp.Diff(tested, expected))

	}
}

func TestConfigReplaceEnvVars(t *testing.T) {
	var testCases = []struct {
		name             string
		file             string
		expectedPassword string
	}{
		{
			"replace env vars with the style of ${}",
			"testdata/envvars.simple.yml",
			"MyPassword",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("CHPROXY_PASSWORD", tc.expectedPassword)

			cfg, err := LoadFile(tc.file)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			got := cfg.Users[0].Password
			if got != tc.expectedPassword {
				t.Fatalf("got password %v; expected to have: %v", got, tc.expectedPassword)
			}
		})
	}
}
