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
	ProviderP2PQuake  ProviderID = "p2pquake"
	ProviderMLIT      ProviderID = "mlit"
	ProviderGDACS     ProviderID = "gdacs"
	ProviderEONET     ProviderID = "eonet"
	ProviderGSI       ProviderID = "gsi"
)

type providerLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	lastRequest time.Time
	blockedUtil time.Time
}

// HTTPCacheEntry stores ETag and Last-Modified headers for conditional HTTP GET requests.
type HTTPCacheEntry struct {
	ETag         string
	LastModified string
}

// HTTPCache caches conditional GET validators per URL to minimize bandwidth and upstream load.
type HTTPCache struct {
	mu      sync.RWMutex
	entries map[string]HTTPCacheEntry
}

// NewHTTPCache creates a thread-safe HTTPCache.
func NewHTTPCache() *HTTPCache {
	return &HTTPCache{
		entries: make(map[string]HTTPCacheEntry),
	}
}

func (c *HTTPCache) get(url string) (HTTPCacheEntry, bool) {
	if c == nil {
		return HTTPCacheEntry{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[url]
	return e, ok
}

func (c *HTTPCache) set(url string, e HTTPCacheEntry) {
	if c == nil || (e.ETag == "" && e.LastModified == "") {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[url] = e
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
			ProviderP2PQuake:  {minInterval: 3000 * time.Millisecond},
			ProviderMLIT:      {minInterval: 2000 * time.Millisecond},
			ProviderGDACS:     {minInterval: 3000 * time.Millisecond},
			ProviderEONET:     {minInterval: 2000 * time.Millisecond},
			ProviderGSI:       {minInterval: 1500 * time.Millisecond},
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
	body, _, err := fetchBytesConditionalWithLimit(ctx, client, rl, provider, nil, url, 16*1024*1024)
	return body, err
}

func fetchBytesConditional(ctx context.Context, client *http.Client, rl *RateLimiter, provider ProviderID, cache *HTTPCache, url string) ([]byte, bool, error) {
	return fetchBytesConditionalWithLimit(ctx, client, rl, provider, cache, url, 16*1024*1024)
}

func fetchBytesConditionalWithLimit(ctx context.Context, client *http.Client, rl *RateLimiter, provider ProviderID, cache *HTTPCache, url string, maxBytes int64) ([]byte, bool, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := rl.Wait(ctx, provider); err != nil {
			return nil, false, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, false, err
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "application/json, application/xml, text/xml, */*")
		if entry, ok := cache.get(url); ok {
			if entry.ETag != "" {
				req.Header.Set("If-None-Match", entry.ETag)
			}
			if entry.LastModified != "" {
				req.Header.Set("If-Modified-Since", entry.LastModified)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(400))*time.Millisecond
			rl.SetBackoff(provider, backoff)
			continue
		}

		if resp.StatusCode == http.StatusNotModified {
			_ = resp.Body.Close()
			return nil, true, nil
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return nil, false, readErr
			}
			cache.set(url, HTTPCacheEntry{
				ETag:         resp.Header.Get("ETag"),
				LastModified: resp.Header.Get("Last-Modified"),
			})
			return body, false, nil
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
		return nil, false, statusErr
	}

	var statusErr *HTTPStatusError
	if errors.As(lastErr, &statusErr) {
		return nil, false, statusErr
	}
	return nil, false, lastErr
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
