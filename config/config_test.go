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
				ListenAddr: ":9090",
				LogDebug:   true,
				Clusters: []Cluster{
					{
						Name:   "first cluster",
						Scheme: "http",
						Nodes:  []string{"127.0.0.1:8123", "127.0.0.2:8123", "127.0.0.3:8123"},
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
						Name:   "second cluster",
						Scheme: "https",
						Nodes:  []string{"127.0.1.1:8123", "127.0.1.2:8123"},
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
						AllowedNetworks: []*Network{
							{
								IPNet: &net.IPNet{
									net.IPv4(127, 0, 0, 1),
									net.IPMask{255, 255, 255, 255},
								},
							},
							{
								IPNet: &net.IPNet{
									net.IPv4(1, 2, 3, 0),
									net.IPMask{255, 255, 255, 0},
								},
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
				ListenAddr: ":8080",
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
