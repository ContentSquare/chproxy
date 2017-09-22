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
					HTTP: HTTP{
						ListenAddr: ":9090",
						Networks: Networks{
							&net.IPNet{
								IP:   net.IPv4(127, 0, 0, 0),
								Mask: net.IPMask{255, 255, 255, 0},
							},
						},
					},
					HTTPS: HTTPS{
						ListenAddr: ":443",
						Autocert: Autocert{
							CacheDir:     "certs_dir",
							AllowedHosts: []string{"example.com"},
						},
						Networks: Networks{
							&net.IPNet{
								IP:   net.IPv4(127, 0, 0, 0),
								Mask: net.IPMask{255, 255, 255, 0},
							},
						},
					},
					Metrics: Metrics{
						Networks: Networks{
							&net.IPNet{
								IP:   net.IPv4(127, 0, 0, 0),
								Mask: net.IPMask{255, 255, 255, 0},
							},
						},
					},
				},
				LogDebug: true,

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
						HeartBeatInterval: time.Minute,
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
						HeartBeatInterval: time.Second * 5,
					},
				},
				Users: []User{
					{
						Name:      "web",
						Password:  "password",
						ToCluster: "second cluster",
						ToUser:    "web",
						DenyHTTP:  true,
					},
					{
						Name:                 "default",
						ToCluster:            "second cluster",
						ToUser:               "default",
						MaxConcurrentQueries: 4,
						MaxExecutionTime:     time.Duration(time.Minute),
						DenyHTTPS:            true,
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
						ListenAddr: ":8080",
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
						KillQueryUser: KillQueryUser{
							Name: "default",
						},
						HeartBeatInterval: time.Second * 5,
					},
				},
				Users: []User{
					{
						Name:      "default",
						Password:  "***",
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
			"empty tls",
			"testdata/bad.empty_tls.yml",
			"configuration `https` is missing. Must be specified `https.cache_dir` for autocert OR `https.key_file` and `https.cert_file` for already existing certs",
		},
		{
			"empty tls cert key",
			"testdata/bad.empty_tls_cert_key.yml",
			"field `https.key_file` must be specified",
		},
		{
			"double certification",
			"testdata/bad.double_certification.yml",
			"it is forbidden to specify certificate and `https.autocert` at the same time. Choose one way",
		},
		{
			"vulnerable user",
			"testdata/bad.vulnerable_user.yml",
			"access for user \"dummy\" must be limited by `password` or by `allowed_networks`",
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
			"user \"dummy\" has both `deny_http` and `deny_https` setted to `true`",
		},
		/*	{
			"security_breach",
			"testdata/bad.security_breach.yml",
			"user \"dummy\" is allowed to connect via http but not limited by `allowed_networks` - possible security breach",
		},*/
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
