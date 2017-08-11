package config

import (
	"gopkg.in/yaml.v2"
	"testing"
	"time"
	"bytes"
)

func TestLoadConfig(t *testing.T) {
	var testCases = []struct {
		name string
		file string
		cfg Config
	}{
		{
			"full description",
			"testdata/full.yml",
			Config{
				Cluster: Cluster{
					Scheme: "http",
					Shards: []string{"localhost:8123"},
				},
				Users: []User{
					{
						Name:                 "web",
						MaxConcurrentQueries: 4,
						MaxExecutionTime:     time.Duration(time.Minute),
					},
					{
						Name:                 "olap",
						MaxConcurrentQueries: 2,
						MaxExecutionTime:     time.Duration(30 * time.Second),
					},
				},
			},
		},
		{
			"default limits value",
			"testdata/default_limits.yml",
			Config{
				Cluster: Cluster{
					Scheme: "http",
					Shards: []string{"localhost:8123"},
				},
				Users: []User{
					{
						Name: "web",
					},
					{
						Name: "olap",
					},
				},
			},
		},
		{
			"default value",
			"testdata/default_user.yml",
			Config{
				Cluster: Cluster{
					Scheme: "http",
					Shards: []string{"localhost:8123"},
				},
				Users: []User{
					{
						Name: "default",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T){
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
		name string
		file string
		error string
	}{
		{
			"no file",
			"testdata/nofile.yml",
			"cannot get file info: stat testdata/nofile.yml: no such file or directory",
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
			"empty shards",
			"testdata/bad.empty_shards.yml",
			"field `shards` must contain at least 1 address",
		},
		{
			"wrong scheme",
			"testdata/bad.wrong_scheme.yml",
			"field `scheme` must be `http` or `https`. Got \"tcp\" instead",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T){
			_, err := LoadFile(tc.file)
			if err.Error() != tc.error {
				t.Fatalf("expected: %q; got: %q", tc.error, err)
			}
		})
	}
}
