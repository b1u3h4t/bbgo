package service

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c9s/bbgo/pkg/fixedpoint"
)

func TestRedisPersistentService(t *testing.T) {
	redisService := NewRedisPersistenceService(&RedisPersistenceConfig{
		Host: "127.0.0.1",
		Port: "6379",
		DB:   0,
	})
	assert.NotNil(t, redisService)

	store := redisService.NewStore("bbgo", "test")
	assert.NotNil(t, store)

	err := store.Reset()
	requireRedis(t, err)

	var fp fixedpoint.Value
	err = store.Load(fp)
	assert.Error(t, err)
	assert.EqualError(t, ErrPersistenceNotExists, err.Error())

	fp = fixedpoint.NewFromFloat(3.1415)
	err = store.Save(&fp)
	requireRedis(t, err)
	assert.NoError(t, err, "should store value without error")

	var fp2 fixedpoint.Value
	err = store.Load(&fp2)
	assert.NoError(t, err, "should load value without error")
	assert.Equal(t, fp, fp2)

	err = store.Reset()
	assert.NoError(t, err)
}

func requireRedis(t *testing.T, err error) {
	t.Helper()
	if err == nil || !isRedisUnavailable(err) {
		assert.NoError(t, err)
		return
	}
	if os.Getenv("GITHUB_CI") != "" {
		t.Fatalf("redis required in CI but unavailable: %v", err)
	}
	t.Skipf("redis not available on 127.0.0.1:6379: %v", err)
}

func isRedisUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout")
}
