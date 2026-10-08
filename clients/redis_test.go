package clients

import (
	"testing"
	"time"

	"github.com/contentsquare/chproxy/config"
)

func TestRedisOptionsDefaults(t *testing.T) {
	opts := redisOptions(config.RedisCacheConfig{Addresses: []string{"127.0.0.1:6379"}})

	if opts.MaxRetries != defaultRedisMaxRetries {
		t.Errorf("expected MaxRetries %d, got %d", defaultRedisMaxRetries, opts.MaxRetries)
	}
	if opts.ConnMaxIdleTime != defaultRedisConnMaxIdleTime {
		t.Errorf("expected ConnMaxIdleTime %s, got %s", defaultRedisConnMaxIdleTime, opts.ConnMaxIdleTime)
	}
	// zero values let go-redis apply its own defaults
	if opts.DialTimeout != 0 || opts.ReadTimeout != 0 || opts.WriteTimeout != 0 || opts.PoolTimeout != 0 {
		t.Errorf("expected zero timeouts, got dial=%s read=%s write=%s pool=%s",
			opts.DialTimeout, opts.ReadTimeout, opts.WriteTimeout, opts.PoolTimeout)
	}
}

func TestRedisOptionsOverrides(t *testing.T) {
	cfg := config.RedisCacheConfig{
		Addresses:       []string{"127.0.0.1:6379"},
		DialTimeout:     config.Duration(2 * time.Second),
		ReadTimeout:     config.Duration(time.Second),
		WriteTimeout:    config.Duration(1500 * time.Millisecond),
		PoolTimeout:     config.Duration(3 * time.Second),
		ConnMaxIdleTime: config.Duration(4 * time.Minute),
		MaxRetries:      -1,
	}

	opts := redisOptions(cfg)

	if opts.DialTimeout != 2*time.Second {
		t.Errorf("expected DialTimeout 2s, got %s", opts.DialTimeout)
	}
	if opts.ReadTimeout != time.Second {
		t.Errorf("expected ReadTimeout 1s, got %s", opts.ReadTimeout)
	}
	if opts.WriteTimeout != 1500*time.Millisecond {
		t.Errorf("expected WriteTimeout 1.5s, got %s", opts.WriteTimeout)
	}
	if opts.PoolTimeout != 3*time.Second {
		t.Errorf("expected PoolTimeout 3s, got %s", opts.PoolTimeout)
	}
	if opts.ConnMaxIdleTime != 4*time.Minute {
		t.Errorf("expected ConnMaxIdleTime 4m, got %s", opts.ConnMaxIdleTime)
	}
	if opts.MaxRetries != -1 {
		t.Errorf("expected MaxRetries -1, got %d", opts.MaxRetries)
	}
}

func TestRedisOptionsDBIndexOnlyForSingleAddress(t *testing.T) {
	single := redisOptions(config.RedisCacheConfig{Addresses: []string{"a:6379"}, DBIndex: 3})
	if single.DB != 3 {
		t.Errorf("expected DB 3 for a single address, got %d", single.DB)
	}

	cluster := redisOptions(config.RedisCacheConfig{Addresses: []string{"a:6379", "b:6379"}, DBIndex: 3})
	if cluster.DB != 0 {
		t.Errorf("expected DB 0 for multiple addresses, got %d", cluster.DB)
	}
}
