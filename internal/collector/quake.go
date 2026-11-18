package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// QuakeClient fetches earthquake & tsunami data from P2PQuake WebSocket/REST, USGS, and JMA.
type QuakeClient struct {
	Client        *http.Client
	P2PWSURL      string
	P2PHistoryURL string
	USGSURL       string
	JMAListURL    string
}

// NewQuakeClient creates a QuakeClient with public earthquake endpoints.
func NewQuakeClient() *QuakeClient {
	return &QuakeClient{
		Client:        newHTTPClient(20 * time.Second),
		P2PWSURL:      "wss://api.p2pquake.net/v2/ws",
		P2PHistoryURL: "https://api.p2pquake.net/v2/history?codes=551&limit=20",
		USGSURL:       "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_day.geojson",
		JMAListURL:    "https://www.jma.go.jp/bosai/quake/data/list.json",
	}
}

type p2pQuakeMessage struct {
	ID         string `json:"id"`
	Code       int    `json:"code"`
	Time       string `json:"time"`
	Earthquake struct {
		Time       string `json:"time"`
		MaxScale   int    `json:"maxScale"`
		DomesticTsunami string `json:"domesticTsunami"`
		Hypocenter struct {
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Depth     float64 `json:"depth"`
			Magnitude float64 `json:"magnitude"`
		} `json:"hypocenter"`
	} `json:"earthquake"`
}

// ParseP2PMessage normalizes a raw P2PQuake JSON payload into a QuakeEvent.
func ParseP2PMessage(raw []byte) (QuakeEvent, bool, error) {
	var msg p2pQuakeMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return QuakeEvent{}, false, err
	}
	if msg.Code != 551 && msg.Code != 552 && msg.Code != 556 {
		return QuakeEvent{}, false, nil
	}

	ts := time.Now().UTC()
	if parsed, err := time.Parse("2006/01/02 15:04:05.000", msg.Time); err == nil {
		ts = parsed
	} else if parsed, err := time.Parse("2006/01/02 15:04:05", msg.Time); err == nil {
		ts = parsed
	}

	scale := formatP2PScale(msg.Earthquake.MaxScale)
	title := fmt.Sprintf("地震 %s M%.1f 最大震度%s",
		msg.Earthquake.Hypocenter.Name,
		msg.Earthquake.Hypocenter.Magnitude,
		scale,
	)
	if msg.Code == 552 {
		title = "津波予報発表"
	} else if msg.Code == 556 {
		title = "緊急地震速報 (EEW)"
	}

	return QuakeEvent{
		ID:         msg.ID,
		Source:     "p2pquake",
		Code:       msg.Code,
		Title:      title,
		Hypocenter: msg.Earthquake.Hypocenter.Name,
		Magnitude:  msg.Earthquake.Hypocenter.Magnitude,
		MaxScale:   scale,
		Latitude:   msg.Earthquake.Hypocenter.Latitude,
		Longitude:  msg.Earthquake.Hypocenter.Longitude,
		DepthKm:    msg.Earthquake.Hypocenter.Depth,
		Tsunami:    msg.Earthquake.DomesticTsunami,
		OccurredAt: ts,
	}, true, nil
}

func formatP2PScale(scale int) string {
	switch scale {
	case 10:
		return "1"
	case 20:
		return "2"
	case 30:
		return "3"
	case 40:
		return "4"
	case 45:
		return "5弱"
	case 50:
		return "5強"
	case 55:
		return "6弱"
	case 60:
		return "6強"
	case 70:
		return "7"
	default:
		return "-"
	}
}

// FetchP2PHistory fetches recent earthquake reports for startup or mobile resume catch-up.
func (c *QuakeClient) FetchP2PHistory(ctx context.Context) ([]QuakeEvent, error) {
	raw, err := fetchBytes(ctx, c.Client, c.P2PHistoryURL)
	if err != nil {
		return nil, err
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	out := make([]QuakeEvent, 0, len(msgs))
	for _, m := range msgs {
		if ev, ok, err := ParseP2PMessage(m); err == nil && ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

type usgsGeoJSON struct {
	Features []struct {
		ID         string `json:"id"`
		Properties struct {
			Mag   float64 `json:"mag"`
			Place string  `json:"place"`
			Time  int64   `json:"time"`
			Title string  `json:"title"`
		} `json:"properties"`
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

// FetchUSGS retrieves global M4.5+ earthquakes from the past 24 hours.
func (c *QuakeClient) FetchUSGS(ctx context.Context) ([]QuakeEvent, error) {
	var geo usgsGeoJSON
	if err := fetchJSON(ctx, c.Client, c.USGSURL, &geo); err != nil {
		return nil, err
	}
	out := make([]QuakeEvent, 0, len(geo.Features))
	for _, f := range geo.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		depth := 0.0
		if len(f.Geometry.Coordinates) >= 3 {
			depth = f.Geometry.Coordinates[2]
		}
		out = append(out, QuakeEvent{
			ID:         "usgs-" + f.ID,
			Source:     "usgs",
			Title:      f.Properties.Title,
			Hypocenter: f.Properties.Place,
			Magnitude:  f.Properties.Mag,
			MaxScale:   "-",
			Longitude:  f.Geometry.Coordinates[0],
			Latitude:   f.Geometry.Coordinates[1],
			DepthKm:    depth,
			OccurredAt: time.UnixMilli(f.Properties.Time).UTC(),
		})
	}
	return out, nil
}

// ListenP2PWebSocket maintains a reconnecting WebSocket stream until ctx is canceled.
func (c *QuakeClient) ListenP2PWebSocket(ctx context.Context, reconnectCh <-chan struct{}, onEvent func(QuakeEvent)) {
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	header := http.Header{"User-Agent": []string{defaultUserAgent}}

	for {
		if ctx.Err() != nil {
			return
		}

		conn, _, err := dialer.DialContext(ctx, c.P2PWSURL, header)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-reconnectCh:
			case <-time.After(10 * time.Second):
			}
			continue
		}

		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if ev, ok, err := ParseP2PMessage(msg); err == nil && ok && onEvent != nil {
					onEvent(ev)
				}
			}
		}()

		select {
		case <-ctx.Done():
			_ = conn.Close()
			<-readDone
			return
		case <-reconnectCh:
			_ = conn.Close()
			<-readDone
		case <-readDone:
			_ = conn.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}
