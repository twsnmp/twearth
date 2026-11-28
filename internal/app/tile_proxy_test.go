package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseOSMTilePath(t *testing.T) {
	tests := []struct {
		path string
		ok   bool
		z    int
		x    int
		y    int
	}{
		{"/api/tiles/osm/0/0/0.png", true, 0, 0, 0},
		{"/api/tiles/osm/5/28/12.png", true, 5, 28, 12},
		{"/api/tiles/osm/19/524287/524287.png", true, 19, 524287, 524287},
		{"/api/tiles/osm/0/1/0.png", false, 0, 0, 0},
		{"/api/tiles/osm/20/0/0.png", false, 0, 0, 0},
		{"/api/tiles/osm/-1/0/0.png", false, 0, 0, 0},
		{"/api/tiles/osm/5/28/12.jpg", false, 0, 0, 0},
		{"/api/tiles/osm/5/28/12", false, 0, 0, 0},
		{"/api/tiles/osm/5/28", false, 0, 0, 0},
		{"/other/5/28/12.png", false, 0, 0, 0},
	}

	for _, tc := range tests {
		z, x, y, ok := parseOSMTilePath(tc.path)
		if ok != tc.ok {
			t.Fatalf("parseOSMTilePath(%q) ok = %v, want %v", tc.path, ok, tc.ok)
		}
		if ok && (z != tc.z || x != tc.x || y != tc.y) {
			t.Fatalf("parseOSMTilePath(%q) = (%d,%d,%d), want (%d,%d,%d)", tc.path, z, x, y, tc.z, tc.x, tc.y)
		}
	}
}

func TestTileProxySetsUserAgentAndCaches(t *testing.T) {
	var hits atomic.Int32
	var gotUA string
	var gotReferer string
	var gotURL string

	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			hits.Add(1)
			gotUA = r.Header.Get("User-Agent")
			gotReferer = r.Header.Get("Referer")
			gotURL = r.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(bytes.NewBufferString("fake-png-bytes")),
			}, nil
		}),
	}

	proxy := newTileProxyWithConfig(
		"https://tile.openstreetmap.org",
		"twearth/0.2.0 (+https://github.com/twsnmp/twearth)",
		client,
		time.Hour,
		16,
	)

	// First request fetches from upstream.
	req1 := httptest.NewRequest(http.MethodGet, "/api/tiles/osm/5/28/12.png", nil)
	req1.Header.Set("Referer", "http://wails.localhost/")
	rec1 := httptest.NewRecorder()
	proxy.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec1.Code)
	}
	if rec1.Body.String() != "fake-png-bytes" {
		t.Fatalf("body = %q, want fake-png-bytes", rec1.Body.String())
	}
	if gotURL != "https://tile.openstreetmap.org/5/28/12.png" {
		t.Fatalf("upstream URL = %q", gotURL)
	}
	if gotUA != "twearth/0.2.0 (+https://github.com/twsnmp/twearth)" {
		t.Fatalf("upstream User-Agent = %q", gotUA)
	}
	if gotReferer != "" {
		t.Fatalf("upstream Referer = %q, want empty", gotReferer)
	}
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits.Load())
	}

	// Second request hits cache.
	req2 := httptest.NewRequest(http.MethodGet, "/api/tiles/osm/5/28/12.png", nil)
	rec2 := httptest.NewRecorder()
	proxy.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK || rec2.Body.String() != "fake-png-bytes" {
		t.Fatalf("cached response mismatch: %d %q", rec2.Code, rec2.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected cache hit, upstream hits = %d", hits.Load())
	}
}

func TestTileProxyDoesNotCacheErrors(t *testing.T) {
	var hits atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			n := hits.Add(1)
			if n == 1 {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString("blocked")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(bytes.NewBufferString("ok")),
			}, nil
		}),
	}

	proxy := newTileProxyWithConfig("https://tile.openstreetmap.org", "twearth/test", client, time.Hour, 16)

	rec1 := httptest.NewRecorder()
	proxy.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/api/tiles/osm/1/0/0.png", nil))
	if rec1.Code != http.StatusForbidden {
		t.Fatalf("first status = %d, want 403", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	proxy.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/tiles/osm/1/0/0.png", nil))
	if rec2.Code != http.StatusOK || rec2.Body.String() != "ok" {
		t.Fatalf("second status = %d body = %q, want 200 ok", rec2.Code, rec2.Body.String())
	}
}
