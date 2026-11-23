//go:build !android && !ios

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

func newTestService(t *testing.T) (*desktopService, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	col := collector.NewManager(st)
	svc := NewService(Dependencies{
		Store:     st,
		Collector: col,
		Version:   "0.2.0-test",
	}).(*desktopService)
	return svc, st
}

func TestLoopbackOnlyMiddleware(t *testing.T) {
	svc, _ := newTestService(t)
	handler := svc.newHandler()

	tests := []struct {
		name       string
		remoteAddr string
		host       string
		origin     string
		wantCode   int
	}{
		{
			name:       "loopback_ipv4_localhost",
			remoteAddr: "127.0.0.1:54321",
			host:       "localhost:18088",
			wantCode:   http.StatusOK,
		},
		{
			name:       "loopback_ipv6",
			remoteAddr: "[::1]:54321",
			host:       "[::1]:18088",
			wantCode:   http.StatusOK,
		},
		{
			name:       "loopback_with_local_origin",
			remoteAddr: "127.0.0.1:54321",
			host:       "127.0.0.1:18088",
			origin:     "http://localhost:9245",
			wantCode:   http.StatusOK,
		},
		{
			name:       "reject_non_loopback_remote_ip",
			remoteAddr: "192.168.1.50:54321",
			host:       "127.0.0.1:18088",
			wantCode:   http.StatusForbidden,
		},
		{
			name:       "reject_public_remote_ip",
			remoteAddr: "8.8.8.8:12345",
			host:       "localhost:18088",
			wantCode:   http.StatusForbidden,
		},
		{
			name:       "reject_dns_rebinding_host",
			remoteAddr: "127.0.0.1:54321",
			host:       "evil.example.com:18088",
			wantCode:   http.StatusForbidden,
		},
		{
			name:       "reject_cross_origin",
			remoteAddr: "127.0.0.1:54321",
			host:       "127.0.0.1:18088",
			origin:     "https://evil.example.com",
			wantCode:   http.StatusForbidden,
		},
	}

	pingBody := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18088/mcp", bytes.NewBufferString(pingBody))
			req.RemoteAddr = tc.remoteAddr
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("got status %d, want %d (body: %s)", rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

func TestStreamableHTTPHandshakeAndTools(t *testing.T) {
	svc, st := newTestService(t)

	// Seed sample event, time-series, and disaster lore for tool queries.
	if err := st.SaveEvent(store.EventRecord{
		ID:        "ev-test-1",
		Type:      "TWEARTH-QUAKE",
		Severity:  "warning",
		Title:     "Test Quake",
		Detail:    "Tokyo Bay",
		Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	if err := st.SaveTimeSeries(store.TimeSeriesPoint{
		LocationID:    "tokyo",
		Timestamp:     time.Now().UTC().Add(-1 * time.Hour).Format("200601021504"),
		PressureMSL:   1013.2,
		Temperature2m: 18.5,
	}); err != nil {
		t.Fatalf("SaveTimeSeries: %v", err)
	}
	if err := st.SaveDisasterLoreBatch([]store.DisasterLoreRecord{
		{
			ID:           "lore-1",
			Name:         "テスト洪水碑",
			Prefecture:   "埼玉県",
			Municipality: "富士見市",
			DisasterKind: "洪水",
			Category:     "flood",
			Year:         1947,
			Latitude:     35.85,
			Longitude:    139.55,
		},
	}); err != nil {
		t.Fatalf("SaveDisasterLoreBatch: %v", err)
	}

	handler := svc.newHandler()
	const mcpURL = "http://127.0.0.1:18088/mcp"

	doReq := func(method, sid string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, mcpURL, bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "127.0.0.1:18088"
		req.Header.Set("Content-Type", "application/json")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`
	resp := doReq(http.MethodPost, "", []byte(initReq))
	if resp.Code != http.StatusOK {
		t.Fatalf("initialize status = %d", resp.Code)
	}
	sid := resp.Header().Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("expected Mcp-Session-Id header")
	}

	// 2. notifications/initialized -> 202 Accepted
	notifResp := doReq(http.MethodPost, sid, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if notifResp.Code != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", notifResp.Code)
	}

	// 3. tools/list -> 14 tools
	listResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	var listResult struct {
		Tools []toolDefinition `json:"tools"`
	}
	mustUnmarshalResult(t, listResp, &listResult)
	if len(listResult.Tools) != 14 {
		t.Fatalf("got %d tools, want 14", len(listResult.Tools))
	}

	// 4. Call get_earth_summary
	sumResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_earth_summary",
			"arguments": map[string]any{},
		},
	})
	var callRes toolCallResult
	mustUnmarshalResult(t, sumResp, &callRes)
	if callRes.IsError || len(callRes.Content) == 0 {
		t.Fatalf("get_earth_summary failed: %+v", callRes)
	}

	// 5. Call list_alert_events
	evResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "list_alert_events",
			"arguments": map[string]any{
				"min_severity": "warning",
			},
		},
	})
	mustUnmarshalResult(t, evResp, &callRes)
	if callRes.IsError || !bytes.Contains([]byte(callRes.Content[0].Text), []byte("ev-test-1")) {
		t.Fatalf("unexpected list_alert_events result: %+v", callRes)
	}

	// 6. Call analyze_environment_history
	histResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "analyze_environment_history",
			"arguments": map[string]any{
				"kind":        "weather",
				"location_id": "tokyo",
				"metric":      "temperature2m",
			},
		},
	})
	mustUnmarshalResult(t, histResp, &callRes)
	if callRes.IsError || !bytes.Contains([]byte(callRes.Content[0].Text), []byte("18.5")) {
		t.Fatalf("unexpected analyze_environment_history result: %+v", callRes)
	}

	// 7. Call search_disaster_lore
	loreResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_disaster_lore",
			"arguments": map[string]any{
				"query": "富士見",
			},
		},
	})
	mustUnmarshalResult(t, loreResp, &callRes)
	if callRes.IsError || !bytes.Contains([]byte(callRes.Content[0].Text), []byte("lore-1")) {
		t.Fatalf("unexpected search_disaster_lore result: %+v", callRes)
	}

	// 8. Call search_and_manage_locations (add_city + remove_city)
	addResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_and_manage_locations",
			"arguments": map[string]any{
				"action":    "add_city",
				"name":      "Sapporo",
				"latitude":  43.0621,
				"longitude": 141.3544,
			},
		},
	})
	mustUnmarshalResult(t, addResp, &callRes)
	if callRes.IsError || !bytes.Contains([]byte(callRes.Content[0].Text), []byte("Sapporo")) {
		t.Fatalf("unexpected add_city result: %+v", callRes)
	}
	var addOut struct {
		Added struct {
			ID string `json:"id"`
		} `json:"added"`
	}
	if err := json.Unmarshal([]byte(callRes.Content[0].Text), &addOut); err != nil {
		t.Fatalf("unmarshal add_city result: %v", err)
	}
	if addOut.Added.ID == "" {
		t.Fatalf("expected added city ID, got empty")
	}

	rmResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_and_manage_locations",
			"arguments": map[string]any{
				"action":      "remove_city",
				"location_id": addOut.Added.ID,
			},
		},
	})
	mustUnmarshalResult(t, rmResp, &callRes)
	if callRes.IsError || !bytes.Contains([]byte(callRes.Content[0].Text), []byte(addOut.Added.ID)) {
		t.Fatalf("unexpected remove_city result: %+v", callRes)
	}
	if bytes.Contains([]byte(callRes.Content[0].Text), []byte("Sapporo")) {
		var rmOut struct {
			Locations []struct {
				ID string `json:"id"`
			} `json:"locations"`
		}
		if err := json.Unmarshal([]byte(callRes.Content[0].Text), &rmOut); err != nil {
			t.Fatalf("unmarshal remove_city result: %v", err)
		}
		for _, loc := range rmOut.Locations {
			if loc.ID == addOut.Added.ID {
				t.Fatalf("expected removed city %q to be absent from locations", addOut.Added.ID)
			}
		}
	}

	rmMissingResp := callRPC(t, handler, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_and_manage_locations",
			"arguments": map[string]any{
				"action":      "remove_city",
				"location_id": "non-existent-id",
			},
		},
	})
	mustUnmarshalResult(t, rmMissingResp, &callRes)
	if !callRes.IsError {
		t.Fatalf("expected error when removing missing city, got: %+v", callRes)
	}

	// 9. DELETE /mcp
	delResp := doReq(http.MethodDelete, sid, nil)
	if delResp.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d", delResp.Code)
	}
}

func TestConfigureStartStop(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Pick an ephemeral port by checking Configure with a high port.
	if err := svc.Configure(ctx, true, 18999); err != nil {
		t.Fatalf("Configure(enable): %v", err)
	}
	st := svc.GetStatus()
	if !st.Running || st.Port != 18999 || st.Endpoint != "http://127.0.0.1:18999/mcp" {
		t.Fatalf("unexpected status after start: %+v", st)
	}

	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st = svc.GetStatus()
	if st.Running {
		t.Fatalf("expected Running=false after Stop: %+v", st)
	}
}

func callRPC(t *testing.T, handler http.Handler, sid string, payload any) jsonRPCResponse {
	t.Helper()
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18088/mcp", bytes.NewReader(b))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "127.0.0.1:18088"
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var out jsonRPCResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode rpc response: %v", err)
	}
	if out.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", out.Error)
	}
	return out
}

func mustUnmarshalResult(t *testing.T, resp jsonRPCResponse, dst any) {
	t.Helper()
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
}
