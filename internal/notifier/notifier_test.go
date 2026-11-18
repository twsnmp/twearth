package notifier

import (
	"strings"
	"testing"
	"time"

	"github.com/twsnmp/twearth/internal/store"
)

func TestShouldNotifyAndFormatSyslog(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.SyslogEnabled = true
	cfg.SyslogHost = "127.0.0.1"

	if !ShouldNotify(cfg, false) {
		t.Fatal("expected desktop notification to be enabled")
	}
	if ShouldNotify(cfg, true) {
		t.Fatal("expected mobile notification to be disabled when NotifyOnMobile=false")
	}
	cfg.NotifyOnMobile = true
	if !ShouldNotify(cfg, true) {
		t.Fatal("expected mobile notification to be enabled when NotifyOnMobile=true")
	}

	ev := store.EventRecord{
		Type:     "TWEARTH-LIGHTNING",
		Severity: "warning",
		Title:    "自宅20km圏内で落雷検知 (3件)",
		Detail:   "LIDEN",
	}
	pkt := FormatSyslogPacket(ev, time.Date(2026, 11, 19, 5, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(pkt, "<12>") || !strings.Contains(pkt, "TWEARTH-LIGHTNING") {
		t.Fatalf("unexpected syslog packet: %s", pkt)
	}

	if IntensityRank("5強") <= IntensityRank("4") {
		t.Fatal("expected 5強 > 4")
	}
}
