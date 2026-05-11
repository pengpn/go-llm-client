package server

import (
	"sync"
	"time"
)

// userBucket 记录某个用户在滑动窗口内的请求时间戳。
type userBucket struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// allow 检查是否允许本次请求。
// 使用滑动窗口算法：只统计窗口内的请求数，超出 limit 时返回 false。
// 每次调用都会清理窗口外的旧时间戳，防止内存持续增长。
func (b *userBucket) allow(limit int, window time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// 不可变原则：创建新 slice，只保留窗口内的时间戳
	recent := make([]time.Time, 0, len(b.timestamps))
	for _, t := range b.timestamps {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= limit {
		b.timestamps = recent // 更新以清理旧数据
		return false
	}

	b.timestamps = append(recent, now)
	return true
}

// RateLimiter 提供基于任意 key 的请求频率限制（滑动窗口算法）。
// 并发安全：每个 key 独立加锁，不同用户互不干扰。
type RateLimiter struct {
	limit   int
	window  time.Duration
	buckets sync.Map // key: string → *userBucket
}

// NewRateLimiter 创建频率限制器。
// limit：时间窗口内允许的最大请求数，window：时间窗口大小。
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window}
}

// Allow 检查 key 是否允许本次请求。超限时返回 false。
// 同一个 key 的请求在窗口内超过 limit 次后会被拒绝，
// 直到窗口内最早的请求滑出窗口。
func (r *RateLimiter) Allow(key string) bool {
	val, _ := r.buckets.LoadOrStore(key, &userBucket{})
	return val.(*userBucket).allow(r.limit, r.window)
}
