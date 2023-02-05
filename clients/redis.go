package clients

import (
	"context"
	"fmt"

	"github.com/contentsquare/chproxy/config"
	"github.com/redis/go-redis/v9"
)

// TODO Implement TLS Client
func NewRedisClient(cfg config.RedisCacheConfig) (redis.UniversalClient, error) {
	r := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:      cfg.Addresses,
		Username:   cfg.Username,
		Password:   cfg.Password,
		MaxRetries: 7, // default value = 3, ince MinRetryBackoff = 8 msec & MinRetryBackoff = 512 msec
		// the redisclient will wait up to 1016 msec btw the 7 tries
	})

	err := r.Ping(context.Background()).Err()

	if err != nil {
		return nil, fmt.Errorf("failed to reach redis: %w", err)
	}

	return r, nil
}
