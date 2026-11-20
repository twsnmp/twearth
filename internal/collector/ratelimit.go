package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ProviderID identifies an upstream API provider for rate limiting.
type ProviderID string

const (
	ProviderOpenMeteo ProviderID = "openmeteo"
	ProviderJMA       ProviderID = "jma"
	ProviderUSGS      ProviderID = "usgs"
	ProviderNOAA      ProviderID = "noaa"
)

type providerLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	lastRequest time.Time
	blockedUtil time.Time
}

// RateLimiter enforces per-provider request pacing and Retry-After backoff.
type RateLimiter struct {
	limiters map[ProviderID]*providerLimiter
}

// NewRateLimiter creates a RateLimiter with sensible defaults for public APIs.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiters: map[ProviderID]*providerLimiter{
			ProviderOpenMeteo: {minInterval: 1200 * time.Millisecond},
			ProviderJMA:       {minInterval: 800 * time.Millisecond},
			ProviderUSGS:      {minInterval: 1000 * time.Millisecond},
			ProviderNOAA:      {minInterval: 1000 * time.Millisecond},
		},
	}
}

// Wait blocks until the provider is ready for the next request or ctx is canceled.
func (r *RateLimiter) Wait(ctx context.Context, provider ProviderID) error {
	if r == nil {
		return nil
	}
	lim, ok := r.limiters[provider]
	if !ok {
		return nil
	}

	for {
		lim.mu.Lock()
		now := time.Now()
		nextAllowed := lim.lastRequest.Add(lim.minInterval)
		if lim.blockedUtil.After(nextAllowed) {
			nextAllowed = lim.blockedUtil
		}
		if !now.Before(nextAllowed) {
			lim.lastRequest = now
			lim.mu.Unlock()
			return nil
		}
		waitDur := nextAllowed.Sub(now)
		lim.mu.Unlock()

		timer := time.NewTimer(waitDur)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// SetBackoff records a rate-limit / server-busy cooldown window for a provider.
func (r *RateLimiter) SetBackoff(provider ProviderID, d time.Duration) {
	if r == nil || d <= 0 {
		return
	}
	lim, ok := r.limiters[provider]
	if !ok {
		return
	}
	lim.mu.Lock()
	defer lim.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(lim.blockedUtil) {
		lim.blockedUtil = until
	}
}

// HTTPStatusError represents a non-2xx HTTP response.
type HTTPStatusError struct {
	StatusCode int
	URL        string
	RetryAfter time.Duration
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("http %d from %s", e.StatusCode, e.URL)
}

func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func fetchBytesLimited(ctx context.Context, client *http.Client, rl *RateLimiter, provider ProviderID, url string) ([]byte, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := rl.Wait(ctx, provider); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "application/json, application/xml, */*")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(400))*time.Millisecond
			rl.SetBackoff(provider, backoff)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return nil, readErr
			}
			return body, nil
		}

		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		statusErr := &HTTPStatusError{
			StatusCode: resp.StatusCode,
			URL:        url,
			RetryAfter: retryAfter,
		}
		lastErr = statusErr

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			wait := retryAfter
			if wait <= 0 {
				wait = time.Duration(2<<attempt)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
			}
			rl.SetBackoff(provider, wait)
			continue
		}
		return nil, statusErr
	}

	var statusErr *HTTPStatusError
	if errors.As(lastErr, &statusErr) {
		return nil, statusErr
	}
	return nil, lastErr
}

func fetchJSONLimited(ctx context.Context, client *http.Client, rl *RateLimiter, provider ProviderID, url string, dst any) error {
	body, err := fetchBytesLimited(ctx, client, rl, provider, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode json from %s: %w", url, err)
	}
	return nil
}
