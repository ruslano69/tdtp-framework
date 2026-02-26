package infra

import (
	"context"
	"fmt"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/ldap"
)

// Infra holds all live infrastructure handles for the running service.
type Infra struct {
	MercuryRedis  *redis.Client
	PipelineRedis *redis.Client
	LDAP          ldap.Client // always a CachingClient wrapping mock or real

	// dev-mode internal instances; nil in production
	miniMercury  *miniredis.Miniredis
	miniPipeline *miniredis.Miniredis
}

// Setup initialises Redis and LDAP.
//   - dev=true: starts two in-process miniredis instances and uses MockClient.
//   - dev=false: connects to the addresses from cfg, uses RealClient.
func Setup(cfg *Config, dev bool) (*Infra, error) {
	inf := &Infra{}

	if dev {
		var err error
		inf.miniMercury, err = miniredis.Run()
		if err != nil {
			return nil, fmt.Errorf("infra: miniredis mercury: %w", err)
		}
		inf.miniPipeline, err = miniredis.Run()
		if err != nil {
			inf.miniMercury.Close()
			return nil, fmt.Errorf("infra: miniredis pipeline: %w", err)
		}

		inf.MercuryRedis = redis.NewClient(&redis.Options{Addr: inf.miniMercury.Addr()})
		inf.PipelineRedis = redis.NewClient(&redis.Options{Addr: inf.miniPipeline.Addr()})

		mock, err := ldap.NewMockClient(cfg.LDAP.MockUsersFile)
		if err != nil {
			return nil, fmt.Errorf("infra: mock ldap: %w", err)
		}
		inf.LDAP = ldap.NewCachingClient(mock, inf.PipelineRedis, cfg.LDAP.CacheTTL)

		log.Info().
			Str("mercury_redis", inf.miniMercury.Addr()).
			Str("pipeline_redis", inf.miniPipeline.Addr()).
			Msg("dev: in-process miniredis started")
	} else {
		inf.MercuryRedis = redis.NewClient(&redis.Options{
			Addr:     cfg.Mercury.Addr,
			Password: cfg.Mercury.Password,
			DB:       cfg.Mercury.DB,
		})
		inf.PipelineRedis = redis.NewClient(&redis.Options{
			Addr:     cfg.Pipeline.Addr,
			Password: cfg.Pipeline.Password,
			DB:       cfg.Pipeline.DB,
		})

		real, err := ldap.NewRealClient(cfg.LDAP)
		if err != nil {
			return nil, fmt.Errorf("infra: ldap connect: %w", err)
		}
		inf.LDAP = ldap.NewCachingClient(real, inf.PipelineRedis, cfg.LDAP.CacheTTL)
	}

	// Verify both Redis connections are alive.
	ctx := context.Background()
	if err := inf.MercuryRedis.Ping(ctx).Err(); err != nil {
		inf.Close()
		return nil, fmt.Errorf("infra: mercury redis ping: %w", err)
	}
	if err := inf.PipelineRedis.Ping(ctx).Err(); err != nil {
		inf.Close()
		return nil, fmt.Errorf("infra: pipeline redis ping: %w", err)
	}

	return inf, nil
}

// Close releases all infrastructure resources.
func (inf *Infra) Close() {
	if inf.LDAP != nil {
		_ = inf.LDAP.Close()
	}
	if inf.MercuryRedis != nil {
		_ = inf.MercuryRedis.Close()
	}
	if inf.PipelineRedis != nil {
		_ = inf.PipelineRedis.Close()
	}
	if inf.miniMercury != nil {
		inf.miniMercury.Close()
	}
	if inf.miniPipeline != nil {
		inf.miniPipeline.Close()
	}
}
