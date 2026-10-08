package clients

import (
	"context"
	"fmt"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/redis/go-redis/v9"
)

const (
	// default value = 3, since MinRetryBackoff = 8 msec & MinRetryBackoff = 512 msec
	// the redis client will wait up to 1016 msec btw the 7 tries
	defaultRedisMaxRetries = 7

	// go-redis keeps idle connections for 30 minutes by default, which is longer than
	// the idle timeout of many firewalls/NATs (e.g. 10 minutes on GCP). Silently dropped
	// connections then stall commands until they time out, so recycle them earlier.
	defaultRedisConnMaxIdleTime = 5 * time.Minute
)

func NewRedisClient(cfg config.RedisCacheConfig) (redis.UniversalClient, error) {
	options := redisOptions(cfg)

	// maintain backwards compatibility in case of non-presence of enable_tls
	if len(cfg.CertFile) != 0 || len(cfg.KeyFile) != 0 || cfg.EnableTLS {
		tlsConfig, err := cfg.TLS.BuildTLSConfig(nil)
		if err != nil {
			return nil, err
		}
		options.TLSConfig = tlsConfig
	}

	r := redis.NewUniversalClient(options)

	err := r.Ping(context.Background()).Err()

	if err != nil {
		return nil, fmt.Errorf("failed to reach redis: %w", err)
	}

	return r, nil
}

func redisOptions(cfg config.RedisCacheConfig) *redis.UniversalOptions {
	options := &redis.UniversalOptions{
		Addrs:           cfg.Addresses,
		Username:        cfg.Username,
		Password:        cfg.Password,
		PoolSize:        cfg.PoolSize,
		MaxRetries:      cfg.MaxRetries,
		DialTimeout:     time.Duration(cfg.DialTimeout),
		ReadTimeout:     time.Duration(cfg.ReadTimeout),
		WriteTimeout:    time.Duration(cfg.WriteTimeout),
		PoolTimeout:     time.Duration(cfg.PoolTimeout),
		ConnMaxIdleTime: time.Duration(cfg.ConnMaxIdleTime),
	}

	if options.MaxRetries == 0 {
		options.MaxRetries = defaultRedisMaxRetries
	}

	if options.ConnMaxIdleTime == 0 {
		options.ConnMaxIdleTime = defaultRedisConnMaxIdleTime
	}

	if len(cfg.Addresses) == 1 {
		options.DB = cfg.DBIndex
	}

	return options
}
