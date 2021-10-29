package clients

import (
	"context"
	"fmt"
	"github.com/Vertamedia/chproxy/config"
	"github.com/go-redis/redis/v8"
)

func NewRedisClient(cfg config.RedisCacheConfig) (redis.UniversalClient, error) {
	r := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    cfg.Addresses,
		Username: cfg.Username,
		Password: cfg.Password,
	})

	err := r.Ping(context.Background()).Err()

	if err != nil {
		return nil, fmt.Errorf("failed to reach redis: %w", err)
	}

	return r, nil

}
