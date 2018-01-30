package config

import (
	"bytes"
	"gopkg.in/yaml.v2"
	"net"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	var testCases = []struct {
		name string
		file string
		cfg  Config
	}{
		{
			"full description",
			"testdata/full.yml",
			Config{
				Caches: []Cache{
					{
						Name:      "longterm",
						Dir:       "/path/to/longterm/cachedir",
						MaxSize:   ByteSize(100 << 30),
						Expire:    Duration(time.Hour),
						GraceTime: Duration(20 * time.Second),
					},
					{
						Name:    "shortterm",
						Dir:     "/path/to/shortterm/cachedir",
						MaxSize: ByteSize(100 << 20),
						Expire:  Duration(10 * time.Second),
					},
				},
				HackMePlease: true,
				Server: Server{
					HTTP: HTTP{
						ListenAddr:           ":9090",
						NetworksOrGroups:     []string{"office", "reporting-apps", "1.2.3.4"},
						ForceAutocertHandler: true,
					},
					HTTPS: HTTPS{
						ListenAddr: ":443",
						Autocert: Autocert{
							CacheDir:     "certs_dir",
							AllowedHosts: []string{"example.com"},
						},
					},
					Metrics: Metrics{
						NetworksOrGroups: []string{"office"},
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
						HeartBeatInterval: Duration(time.Minute),
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
						HeartBeatInterval: Duration(5 * time.Second),
					},
				},
				Users: []User{
					{
						Name:         "web",
						Password:     "****",
						ToCluster:    "first cluster",
						ToUser:       "web",
						DenyHTTP:     true,
						AllowCORS:    true,
						ReqPerMin:    4,
						MaxQueueSize: 100,
						MaxQueueTime: Duration(35 * time.Second),
						Cache:        "longterm",
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
			},
		},
		{
			"default values",
			"testdata/default_values.yml",
			Config{
				Server: Server{
					HTTP: HTTP{
						ListenAddr:       ":8080",
						NetworksOrGroups: []string{"127.0.0.1"},
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
						HeartBeatInterval: Duration(5 * time.Second),
					},
				},
				Users: []User{
					{
						Name:      "default",
						ToCluster: "cluster",
						ToUser:    "default",
					},
				},
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
				t.Fatalf("unexpected config result: \ngot\n\n%s\n expected\n\n%s", got, exp)
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
			"`letsencrypt` specification requires https server to listen on :443 port and be without `allowed_networks` limits. " +
				"Otherwise, certificates will be impossible to generate",
		},
		{
			"incorrect network group name",
			"testdata/bad.network_groups.yml",
			"wrong network group name or address \"office\": invalid CIDR address: office/32",
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
			"cache max size",
			"testdata/bad.cache_max_size.yml",
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
		v, err := parseDuration(tc.value)
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
		_, err := parseDuration(tc.value)
		if err == nil {
			t.Fatalf("expected to get parse error; got: nil")
		}
		if err.Error() != tc.error {
			t.Fatalf("unexpected error - got: %q; expected: %q", err, tc.error)
		}
	}
}
