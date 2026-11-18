package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gosnmp/gosnmp"
	"github.com/twsnmp/twearth/internal/platform"
	"github.com/twsnmp/twearth/internal/store"
)

// Dispatcher sends alert events to TWSNMP via Syslog, MQTT, SNMP Trap, and Webhook.
type Dispatcher struct {
	httpClient *http.Client
}

// NewDispatcher creates a new notification dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ShouldNotify returns true if external notifications are allowed on the current platform.
func ShouldNotify(cfg store.Config, isMobile bool) bool {
	if isMobile && !cfg.NotifyOnMobile {
		return false
	}
	return cfg.SyslogEnabled || cfg.MQTTEnabled || cfg.SNMPTrapEnabled || cfg.WebhookEnabled
}

// Send dispatches an EventRecord to all enabled TWSNMP notification channels.
func (d *Dispatcher) Send(ctx context.Context, cfg store.Config, ev store.EventRecord) []error {
	if !ShouldNotify(cfg, platform.GetInfo().IsMobile) {
		return nil
	}

	var errs []error
	if cfg.SyslogEnabled && cfg.SyslogHost != "" {
		if err := sendSyslog(cfg.SyslogHost, cfg.SyslogPort, ev); err != nil {
			errs = append(errs, fmt.Errorf("syslog: %w", err))
		}
	}
	if cfg.MQTTEnabled && cfg.MQTTBroker != "" {
		if err := sendMQTT(cfg.MQTTBroker, cfg.MQTTTopic, ev); err != nil {
			errs = append(errs, fmt.Errorf("mqtt: %w", err))
		}
	}
	if cfg.SNMPTrapEnabled && cfg.SNMPTrapTarget != "" {
		if err := sendSNMPTrap(cfg.SNMPTrapTarget, cfg.SNMPCommunity, ev); err != nil {
			errs = append(errs, fmt.Errorf("snmp trap: %w", err))
		}
	}
	if cfg.WebhookEnabled && cfg.WebhookURL != "" {
		if err := d.sendWebhook(ctx, cfg.WebhookURL, ev); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}
	return errs
}

// FormatSyslogPacket formats an RFC3164-compatible syslog datagram.
func FormatSyslogPacket(ev store.EventRecord, now time.Time) string {
	pri := 14 // facility 1 (user), severity 6 (info)
	switch strings.ToLower(ev.Severity) {
	case "critical":
		pri = 10 // alert
	case "warning":
		pri = 12 // warning
	}
	ts := now.UTC().Format(time.Stamp)
	return fmt.Sprintf("<%d>%s twearth twearth: [%s] %s - %s", pri, ts, ev.Type, ev.Title, ev.Detail)
}

func sendSyslog(host string, port int, ev store.EventRecord) error {
	if port <= 0 {
		port = 514
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	pkt := FormatSyslogPacket(ev, time.Now())
	_, err = conn.Write([]byte(pkt))
	return err
}

func sendMQTT(broker, topic string, ev store.EventRecord) error {
	if topic == "" {
		topic = "twsnmp/twearth/events"
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("twearth-" + strconv.FormatInt(time.Now().UnixNano(), 36)).
		SetConnectTimeout(5 * time.Second)
	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		if token.Error() != nil {
			return token.Error()
		}
		return fmt.Errorf("mqtt connect timeout")
	}
	defer client.Disconnect(250)

	pub := client.Publish(topic, 0, false, payload)
	if !pub.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout")
	}
	return pub.Error()
}

func sendSNMPTrap(target, community string, ev store.EventRecord) error {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		host = target
		portStr = "162"
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum <= 0 {
		portNum = 162
	}
	if community == "" {
		community = "public"
	}

	g := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(portNum),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   5 * time.Second,
	}
	if err := g.Connect(); err != nil {
		return err
	}
	defer func() { _ = g.Conn.Close() }()

	trap := gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(time.Now().Unix())},
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.17861.10.1"},
			{Name: ".1.3.6.1.4.1.17861.10.1.1", Type: gosnmp.OctetString, Value: ev.Type},
			{Name: ".1.3.6.1.4.1.17861.10.1.2", Type: gosnmp.OctetString, Value: ev.Title},
			{Name: ".1.3.6.1.4.1.17861.10.1.3", Type: gosnmp.OctetString, Value: ev.Detail},
		},
	}
	_, err = g.SendTrap(trap)
	return err
}

func (d *Dispatcher) sendWebhook(ctx context.Context, url string, ev store.EventRecord) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

// IntensityRank maps JMA seismic intensity strings to a comparable integer rank.
func IntensityRank(s string) int {
	switch strings.TrimSpace(s) {
	case "1":
		return 10
	case "2":
		return 20
	case "3":
		return 30
	case "4":
		return 40
	case "5弱":
		return 45
	case "5強":
		return 50
	case "6弱":
		return 55
	case "6強":
		return 60
	case "7":
		return 70
	default:
		return 0
	}
}
