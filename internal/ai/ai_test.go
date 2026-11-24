package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/collector"
	"github.com/twsnmp/twearth/internal/store"
)

func TestBuildLocationPrompt(t *testing.T) {
	cfg := store.DefaultConfig()
	st := collector.LatestState{
		Weathers: []collector.WeatherSnapshot{
			{
				LocationID:         "tokyo",
				LocationName:       "東京",
				Latitude:           35.6762,
				Longitude:          139.6503,
				PressureMSL:        1004.5,
				PressureDelta3h:    -4.2,
				Temperature2m:      21.3,
				RelativeHumidity2m: 78,
				CAPE:               850,
				Timestamp:          time.Now().UTC(),
			},
		},
		Space: &collector.SpaceWeatherSnapshot{
			KpIndex:    4.5,
			FlareClass: "M1.2",
		},
		Liden: &collector.LidenSnapshot{
			NearbyCount: 3,
			TotalCount:  25,
			RadiusKm:    20,
			ValidTime:   "12:00",
		},
	}

	sys, prompt := BuildLocationPrompt(cfg, st, nil, "tokyo", "ja")
	if !strings.Contains(sys, "日本語") {
		t.Errorf("expected Japanese system prompt, got %q", sys)
	}
	if !strings.Contains(prompt, "1004.5 hPa") {
		t.Errorf("expected pressure in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "M1.2") {
		t.Errorf("expected flare class in prompt, got %q", prompt)
	}
}

func TestBuildViewPromptAndQA(t *testing.T) {
	cfg := store.DefaultConfig()
	st := collector.LatestState{
		Synoptic: &collector.SynopticData{
			Centers: []collector.PressureCenter{
				{Kind: "L", Pressure: 992.0, Latitude: 34.0, Longitude: 138.0},
			},
		},
	}

	sys, prompt := BuildViewPrompt(cfg, st, nil, ViewExplainRequest{
		View: "synoptic",
		Lang: "en",
	})
	if !strings.Contains(sys, "meteorology") {
		t.Errorf("expected English system prompt, got %q", sys)
	}
	if !strings.Contains(prompt, "992.0 hPa") {
		t.Errorf("expected low pressure center in synoptic prompt, got %q", prompt)
	}

	qaSys := BuildQASystemPrompt(cfg, st, "tokyo", "ja")
	if !strings.Contains(qaSys, "twearth") {
		t.Errorf("expected twearth in QA system prompt")
	}
}

func TestGetLLMUnconfigured(t *testing.T) {
	cfg := store.DefaultConfig()
	_, err := GetLLM(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when LLMProvider is none")
	}
}
