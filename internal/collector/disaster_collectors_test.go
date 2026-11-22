package collector

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseJmaExtraFeedAndDedupe(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>urn:uuid:1111-2222</id>
    <title>土砂災害警戒情報</title>
    <updated>2026-11-22T04:00:00Z</updated>
    <author><name>東京管区気象台</name></author>
    <link href="https://example.com/1"/>
    <content type="text">【東京都土砂災害警戒情報】八王子市に土砂災害警戒情報を発表しました。</content>
  </entry>
  <entry>
    <id>urn:uuid:3333-4444</id>
    <title>指定河川洪水予報</title>
    <updated>2026-11-22T04:10:00Z</updated>
    <author><name>仙台管区気象台</name></author>
    <content type="text">阿武隈川 氾濫危険情報</content>
  </entry>
</feed>`)

	recs, seen, err := ParseJmaExtraFeed(xmlData, nil)
	if err != nil {
		t.Fatalf("ParseJmaExtraFeed: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Category != "landslide" || recs[0].Severity != "critical" {
		t.Fatalf("unexpected first record: %+v", recs[0])
	}
	if recs[1].Category != "flood" || recs[1].Severity != "critical" {
		t.Fatalf("unexpected second record: %+v", recs[1])
	}

	// Second pass with seen IDs should yield 0 new records.
	recs2, _, err := ParseJmaExtraFeed(xmlData, seen)
	if err != nil {
		t.Fatalf("ParseJmaExtraFeed second pass: %v", err)
	}
	if len(recs2) != 0 {
		t.Fatalf("expected 0 new records on dedupe, got %d", len(recs2))
	}
}

func TestParseMlitRiver(t *testing.T) {
	tableJSON := []byte(`{
		"44132": {"kjName": "東京", "enName": "Tokyo", "lat": [35, 41.5], "lon": [139, 45.0]}
	}`)
	targets, err := ParseAmedasTable(tableJSON)
	if err != nil {
		t.Fatalf("ParseAmedasTable: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 station, got %d", len(targets))
	}

	mapJSON := []byte(`{
		"44132": {"precipitation1h": [55.0, 0], "precipitation24h": [120.0, 0]}
	}`)
	pts, alerts, err := ParseAmedasMap(mapJSON, time.Date(2026, 11, 22, 5, 0, 0, 0, time.UTC), targets)
	if err != nil {
		t.Fatalf("ParseAmedasMap: %v", err)
	}
	if len(pts) != 1 || pts[0].Status != "danger" {
		t.Fatalf("expected danger status, got %+v", pts)
	}
	if len(alerts) != 1 || alerts[0].Severity != "critical" {
		t.Fatalf("expected critical rain/flood alert, got %+v", alerts)
	}
}

func TestParseGDACSAndEONET(t *testing.T) {
	gdacsJSON := []byte(`{
		"type": "FeatureCollection",
		"features": [{
			"type": "Feature",
			"geometry": {"type": "Polygon", "coordinates": [[[100.0, 13.0], [101.0, 13.0], [101.0, 14.0], [100.0, 14.0]]]},
			"properties": {
				"eventid": 9999,
				"eventtype": "FL",
				"name": "Flood in Thailand",
				"alertlevel": "Red",
				"country": "Thailand",
				"fromdate": "2026-11-20T00:00:00Z",
				"todate": "2026-11-22T00:00:00Z",
				"iscurrent": "true",
				"population": 250000
			}
		}]
	}`)
	gdacsRecs, err := ParseGDACSGeoJSON(gdacsJSON)
	if err != nil {
		t.Fatalf("ParseGDACSGeoJSON: %v", err)
	}
	if len(gdacsRecs) != 1 || gdacsRecs[0].Category != "flood" || gdacsRecs[0].Severity != "critical" || len(gdacsRecs[0].Polygon) != 4 {
		t.Fatalf("unexpected gdacs record: %+v", gdacsRecs)
	}

	eonetJSON := []byte(`{
		"events": [{
			"id": "EONET_1234",
			"title": "Wildfire in California",
			"categories": [{"id": "wildfires", "title": "Wildfires"}],
			"geometry": [
				{"date": "2026-11-20T00:00:00Z", "type": "Point", "coordinates": [-118.2, 34.0]},
				{"date": "2026-11-21T00:00:00Z", "type": "Point", "coordinates": [-118.3, 34.1]}
			]
		}]
	}`)
	eonetRecs, err := ParseEONETJSON(eonetJSON)
	if err != nil {
		t.Fatalf("ParseEONETJSON: %v", err)
	}
	if len(eonetRecs) != 1 || eonetRecs[0].Category != "wildfire" || len(eonetRecs[0].Polygon) != 2 {
		t.Fatalf("unexpected eonet record: %+v", eonetRecs)
	}
}

func TestParseGSILoreGeoJSON(t *testing.T) {
	loreJSON := []byte(`{
		"type": "FeatureCollection",
		"features": [{
			"id": "L1",
			"geometry": {"type": "Point", "coordinates": [139.7, 35.7]},
			"properties": {
				"碑名": "明治43年大水害記念碑",
				"災害種別": "洪水",
				"災害名": "1910年関東大水害",
				"伝承内容": "1910年8月の豪雨により氾濫。",
				"都道府県名": "東京都",
				"市区町村名": "足立区"
			}
		}]
	}`)
	recs, err := ParseGSILoreGeoJSON(loreJSON)
	if err != nil {
		t.Fatalf("ParseGSILoreGeoJSON: %v", err)
	}
	if len(recs) != 1 || recs[0].Category != "flood" || recs[0].Year != 1910 {
		t.Fatalf("unexpected lore record: %+v", recs)
	}
}

func TestConditionalHTTP304Cache(t *testing.T) {
	var calls int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Etag": []string{`"v1"`}},
					Body:       io.NopCloser(strings.NewReader(`[]`)),
				}, nil
			}
			if r.Header.Get("If-None-Match") == `"v1"` {
				return &http.Response{
					StatusCode: http.StatusNotModified,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}

	rl := NewRateLimiter()
	cache := NewHTTPCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body, notMod, err := fetchBytesConditional(ctx, client, rl, ProviderGDACS, cache, "https://example.test/feed")
	if err != nil || notMod || string(body) != `[]` {
		t.Fatalf("first fetch unexpected: body=%q notMod=%v err=%v", string(body), notMod, err)
	}

	_, notMod2, err := fetchBytesConditional(ctx, client, rl, ProviderGDACS, cache, "https://example.test/feed")
	if err != nil || !notMod2 {
		t.Fatalf("second fetch expected 304 notModified=true, got notMod=%v err=%v", notMod2, err)
	}
}
