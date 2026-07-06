package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client *redis.Client
}

func New(addr, password string) *Limiter {
	return &Limiter{client: redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
	})}
}

func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.client.Expire(ctx, key, window).Err(); err != nil {
			return false, err
		}
	}
	return count <= int64(limit), nil
}
