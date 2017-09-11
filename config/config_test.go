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
				Server: Server{
					ListenAddr: ":9090",
					IsTLS:      true,
					TLSConfig: TLSConfig{
						CertCacheDir: "certs_dir",
						HostPolicy:   []string{"example.com"},
						CertFile:     "/path/to/cert_file",
						KeyFile:      "/path/to/key_file",
					},
				},
				LogDebug: true,
				Networks: Networks{
					&net.IPNet{
						IP:   net.IPv4(127, 0, 0, 0),
						Mask: net.IPMask{255, 255, 255, 0},
					},
				},
				Clusters: []Cluster{
					{
						Name:   "first cluster",
						Scheme: "http",
						Nodes:  []string{"127.0.0.1:8123", "127.0.0.2:8123", "127.0.0.3:8123"},
						KillQueryUser: KillQueryUser{
							Name:     "default",
							Password: "password",
						},
						ClusterUsers: []ClusterUser{
							{
								Name:                 "web",
								Password:             "password",
								MaxConcurrentQueries: 4,
								MaxExecutionTime:     time.Duration(time.Minute),
							},
						},
					},
					{
						Name:          "second cluster",
						Scheme:        "https",
						Nodes:         []string{"127.0.1.1:8123", "127.0.1.2:8123"},
						KillQueryUser: KillQueryUser{Name: "default"},
						ClusterUsers: []ClusterUser{
							{
								Name:                 "default",
								MaxConcurrentQueries: 4,
								MaxExecutionTime:     time.Duration(time.Minute),
							},
							{
								Name:                 "web",
								MaxConcurrentQueries: 4,
								MaxExecutionTime:     time.Duration(time.Second * 10),
							},
						},
					},
				},
				Users: []User{
					{
						Name:     "web",
						Password: "password",
						Networks: Networks{
							&net.IPNet{
								IP:   net.IPv4(127, 0, 0, 1),
								Mask: net.IPMask{255, 255, 255, 255},
							},
							&net.IPNet{
								IP:   net.IPv4(1, 2, 3, 0),
								Mask: net.IPMask{255, 255, 255, 0},
							},
						},
						ToCluster: "second cluster",
						ToUser:    "web",
					},
					{
						Name:                 "default",
						ToCluster:            "second cluster",
						ToUser:               "default",
						MaxConcurrentQueries: 4,
						MaxExecutionTime:     time.Duration(time.Minute),
					},
				},
			},
		},
		{
			"default values",
			"testdata/default_values.yml",
			Config{
				Server: Server{
					ListenAddr: ":8080",
				},
				Clusters: []Cluster{
					{
						Name:   "second cluster",
						Scheme: "http",
						Nodes:  []string{"127.0.1.1:8123"},
						ClusterUsers: []ClusterUser{
							{
								Name: "default",
							},
						},
						KillQueryUser: KillQueryUser{
							Name: "default",
						},
					},
				},
				Users: []User{
					{
						Name:      "default",
						ToCluster: "second cluster",
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
			"unknown fields in cluster: unknown_field",
		},
		{
			"empty users",
			"testdata/bad.empty_users.yml",
			"field `users` must contain at least 1 user",
		},
		{
			"empty nodes",
			"testdata/bad.empty_nodes.yml",
			"field `cluster.nodes` must contain at least 1 address",
		},
		{
			"wrong scheme",
			"testdata/bad.wrong_scheme.yml",
			"field `cluster.scheme` must be `http` or `https`. Got \"tcp\" instead",
		},
		{
			"empty server",
			"testdata/bad.empty_server.yml",
			"field `server.listen_addr` cannot be empty",
		},
		{
			"empty tls",
			"testdata/bad.empty_tls.yml",
			"configuration `tls_config` is missing. Must be specified `tls_config.cert_cache_dir` for autocert OR `tls_config.key_file` and `tls_config.cert_file` for already existing certs",
		},
		{
			"empty tls cert key",
			"testdata/bad.empty_tls_cert_key.yml",
			"field `tls_config.key_file` must be specified",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFile(tc.file)
			if err.Error() != tc.error {
				t.Fatalf("expected: %q; got: %q", tc.error, err)
			}
		})
	}
}
