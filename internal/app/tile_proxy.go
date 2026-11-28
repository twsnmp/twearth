package app

import (
	"container/list"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	osmTilePathPrefix  = "/api/tiles/osm/"
	defaultOSMBaseURL  = "https://tile.openstreetmap.org"
	defaultTileCacheTTL = 24 * time.Hour
	maxTileCacheItems  = 512
	maxTileBodyBytes   = 4 * 1024 * 1024
	maxUpstreamConc    = 6
)

type cachedTile struct {
	key         string
	body        []byte
	contentType string
	expiresAt   time.Time
}

// TileProxy serves OpenStreetMap raster tiles with a policy-compliant User-Agent and local caching.
type TileProxy struct {
	baseURL   string
	userAgent string
	client    *http.Client
	ttl       time.Duration
	maxItems  int
	sem       chan struct{}

	mu    sync.Mutex
	cache map[string]*list.Element
	lru   *list.List
}

// NewTileProxy creates a new TileProxy targeting OpenStreetMap's tile server.
func NewTileProxy(version string) *TileProxy {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "0.1.0"
	}
	return newTileProxyWithConfig(
		defaultOSMBaseURL,
		fmt.Sprintf("twearth/%s (+https://github.com/twsnmp/twearth)", v),
		&http.Client{Timeout: 15 * time.Second},
		defaultTileCacheTTL,
		maxTileCacheItems,
	)
}

func newTileProxyWithConfig(baseURL, userAgent string, client *http.Client, ttl time.Duration, maxItems int) *TileProxy {
	return &TileProxy{
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: userAgent,
		client:    client,
		ttl:       ttl,
		maxItems:  maxItems,
		sem:       make(chan struct{}, maxUpstreamConc),
		cache:     make(map[string]*list.Element, maxItems),
		lru:       list.New(),
	}
}

// NewTileProxyMiddleware returns a Wails AssetOptions middleware that intercepts /api/tiles/osm/ requests.
func NewTileProxyMiddleware(version string) application.Middleware {
	proxy := NewTileProxy(version)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, osmTilePathPrefix) {
				proxy.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ServeHTTP handles GET/HEAD requests for /api/tiles/osm/{z}/{x}/{y}.png.
func (p *TileProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	z, x, y, ok := parseOSMTilePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	if item, hit := p.getCached(key); hit {
		writeTileResponse(w, r.Method, item.contentType, item.body)
		return
	}

	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-r.Context().Done():
		http.Error(w, "request canceled", http.StatusRequestTimeout)
		return
	}

	// Re-check cache in case a concurrent request populated it while waiting on the semaphore.
	if item, hit := p.getCached(key); hit {
		writeTileResponse(w, r.Method, item.contentType, item.body)
		return
	}

	upstreamURL := fmt.Sprintf("%s/%d/%d/%d.png", p.baseURL, z, x, y)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "invalid upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept", "image/png,image/*;q=0.8,*/*;q=0.5")

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "upstream tile fetch failed", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream returned %d", resp.StatusCode), resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTileBodyBytes))
	if err != nil {
		http.Error(w, "failed to read upstream tile", http.StatusBadGateway)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	p.putCached(key, contentType, body)
	writeTileResponse(w, r.Method, contentType, body)
}

func writeTileResponse(w http.ResponseWriter, method, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (p *TileProxy) getCached(key string) (*cachedTile, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	elem, ok := p.cache[key]
	if !ok {
		return nil, false
	}
	item := elem.Value.(*cachedTile)
	if time.Now().After(item.expiresAt) {
		p.lru.Remove(elem)
		delete(p.cache, key)
		return nil, false
	}
	p.lru.MoveToFront(elem)
	return item, true
}

func (p *TileProxy) putCached(key, contentType string, body []byte) {
	if p.maxItems <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	expiresAt := time.Now().Add(p.ttl)
	if elem, ok := p.cache[key]; ok {
		item := elem.Value.(*cachedTile)
		item.contentType = contentType
		item.body = body
		item.expiresAt = expiresAt
		p.lru.MoveToFront(elem)
		return
	}

	if p.lru.Len() >= p.maxItems {
		if oldest := p.lru.Back(); oldest != nil {
			oldItem := oldest.Value.(*cachedTile)
			delete(p.cache, oldItem.key)
			p.lru.Remove(oldest)
		}
	}

	elem := p.lru.PushFront(&cachedTile{
		key:         key,
		body:        body,
		contentType: contentType,
		expiresAt:   expiresAt,
	})
	p.cache[key] = elem
}

func parseOSMTilePath(path string) (z, x, y int, ok bool) {
	if !strings.HasPrefix(path, osmTilePathPrefix) {
		return 0, 0, 0, false
	}
	rest := strings.TrimPrefix(path, osmTilePathPrefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	if !strings.HasSuffix(parts[2], ".png") {
		return 0, 0, 0, false
	}
	yStr := strings.TrimSuffix(parts[2], ".png")

	var err error
	if !isDigits(parts[0]) || !isDigits(parts[1]) || !isDigits(yStr) {
		return 0, 0, 0, false
	}
	if z, err = strconv.Atoi(parts[0]); err != nil || z < 0 || z > 19 {
		return 0, 0, 0, false
	}
	maxCoord := 1 << z
	if x, err = strconv.Atoi(parts[1]); err != nil || x < 0 || x >= maxCoord {
		return 0, 0, 0, false
	}
	if y, err = strconv.Atoi(yStr); err != nil || y < 0 || y >= maxCoord {
		return 0, 0, 0, false
	}
	return z, x, y, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
