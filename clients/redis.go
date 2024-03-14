package clients

import (
	"context"
	"fmt"

	"github.com/contentsquare/chproxy/config"
	"github.com/redis/go-redis/v9"
)

func NewRedisClient(cfg config.RedisCacheConfig) (redis.UniversalClient, error) {
	options := &redis.UniversalOptions{
		Addrs:      cfg.Addresses,
		Username:   cfg.Username,
		Password:   cfg.Password,
		PoolSize:   cfg.PoolSize,
		MaxRetries: 7, // default value = 3, since MinRetryBackoff = 8 msec & MinRetryBackoff = 512 msec
		// the redis client will wait up to 1016 msec btw the 7 tries
	}

	if len(cfg.Addresses) == 1 {
		options.DB = cfg.DBIndex
	}

	if len(cfg.CertFile) != 0 || len(cfg.KeyFile) != 0 {
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
