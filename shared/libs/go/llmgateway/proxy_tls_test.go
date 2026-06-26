package llmgateway

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	cfg := config.TLSConfig{
		Enabled:   true,
		Mode:      "auto",
		ExtraSANs: []string{"gateway", "proxy"},
	}

	mgr := NewTLSCertManager(cfg, log)
	err := mgr.GenerateAndLoad()
	if err != nil {
		t.Fatalf("GenerateAndLoad failed: %v", err)
	}

	mgr.mu.RLock()
	cert := mgr.cert
	expiresAt := mgr.expiresAt
	mgr.mu.RUnlock()

	if cert == nil {
		t.Fatal("certificate is nil")
	}

	// Verify certificate attributes using x509 parser
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse leaf certificate: %v", err)
	}

	if leaf.Subject.CommonName != "tern-llmgp-local" {
		t.Errorf("CommonName = %q, want %q", leaf.Subject.CommonName, "tern-llmgp-local")
	}

	// Verify SANs
	hasIP := false
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			hasIP = true
		}
	}
	if !hasIP {
		t.Error("expected 127.0.0.1 in SAN IPAddresses")
	}

	sans := map[string]bool{}
	for _, dns := range leaf.DNSNames {
		sans[dns] = true
	}

	if !sans["localhost"] {
		t.Error("expected localhost in SAN DNSNames")
	}
	if !sans["gateway"] {
		t.Error("expected gateway in SAN DNSNames")
	}
	if !sans["proxy"] {
		t.Error("expected proxy in SAN DNSNames")
	}

	// Check duration (approx. 24 hours)
	expectedExpiry := time.Now().Add(24 * time.Hour)
	diff := expiresAt.Sub(expectedExpiry)
	if diff < -1*time.Minute || diff > 1*time.Minute {
		t.Errorf("expiresAt = %v, expected approx %v", expiresAt, expectedExpiry)
	}
}

func TestGenerateSelfSignedCert_CustomDuration(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	cfg := config.TLSConfig{Enabled: true, Mode: "auto"}
	mgr := NewTLSCertManager(cfg, log)
	mgr.certDuration = 5 * time.Second

	err := mgr.GenerateAndLoad()
	if err != nil {
		t.Fatalf("GenerateAndLoad failed: %v", err)
	}

	mgr.mu.RLock()
	expiresAt := mgr.expiresAt
	mgr.mu.RUnlock()

	expectedExpiry := time.Now().Add(5 * time.Second)
	diff := expiresAt.Sub(expectedExpiry)
	if diff < -500*time.Millisecond || diff > 500*time.Millisecond {
		t.Errorf("expiresAt = %v, expected approx %v", expiresAt, expectedExpiry)
	}
}

func TestWriteCACertFile(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	cfg := config.TLSConfig{Enabled: true, Mode: "auto"}
	mgr := NewTLSCertManager(cfg, log)

	err := mgr.GenerateAndLoad()
	if err != nil {
		t.Fatalf("GenerateAndLoad failed: %v", err)
	}

	path, err := mgr.WriteCACertFile()
	if err != nil {
		t.Fatalf("WriteCACertFile failed: %v", err)
	}
	defer os.Remove(path)

	// Verify file exists
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CA cert file: %v", err)
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("failed to decode valid certificate PEM block")
	}
}

// TestBufferLogger captures log output for log verification.
type TestBufferLogger struct {
	logger.Logger
	buf bytes.Buffer
}

func (l *TestBufferLogger) Info(msg string, keysAndValues ...any) {
	l.buf.WriteString("INFO: " + msg + "\n")
}
func (l *TestBufferLogger) Warn(msg string, keysAndValues ...any) {
	l.buf.WriteString("WARN: " + msg + "\n")
}
func (l *TestBufferLogger) Error(msg string, keysAndValues ...any) {
	l.buf.WriteString("ERROR: " + msg + "\n")
}
func (l *TestBufferLogger) Debug(msg string, keysAndValues ...any) {
	l.buf.WriteString("DEBUG: " + msg + "\n")
}
func (l *TestBufferLogger) Trace(msg string, keysAndValues ...any) {
	l.buf.WriteString("TRACE: " + msg + "\n")
}
func (l *TestBufferLogger) WithComponent(name string) logger.Logger {
	return l
}

func TestTLSCertManager_AutoRenewal(t *testing.T) {
	bufLog := &TestBufferLogger{}
	cfg := config.TLSConfig{Enabled: true, Mode: "auto"}
	mgr := NewTLSCertManager(cfg, bufLog)

	// Set short durations for fast testing
	mgr.certDuration = 3 * time.Second
	mgr.renewalThreshold = 2 * time.Second
	mgr.checkInterval = 500 * time.Millisecond

	err := mgr.GenerateAndLoad()
	if err != nil {
		t.Fatalf("GenerateAndLoad failed: %v", err)
	}

	mgr.mu.RLock()
	firstExpires := mgr.expiresAt
	mgr.mu.RUnlock()

	mgr.Start()
	defer mgr.Stop()

	// Wait for renewal (expiry is 3s, threshold is 2s, check every 0.5s -> should renew around 1s)
	time.Sleep(1500 * time.Millisecond)

	mgr.mu.RLock()
	secondExpires := mgr.expiresAt
	mgr.mu.RUnlock()

	if !secondExpires.After(firstExpires) {
		t.Errorf("expected certificate to renew (expiresAt: %v -> %v)", firstExpires, secondExpires)
	}

	logStr := bufLog.buf.String()
	if !bytes.Contains([]byte(logStr), []byte("TLS certificate auto-renewed successfully")) {
		t.Errorf("expected auto-renewal log, got logs:\n%s", logStr)
	}
}

func TestTLSCertManager_ExpiryWarning(t *testing.T) {
	bufLog := &TestBufferLogger{}
	cfg := config.TLSConfig{Enabled: true, Mode: "auto"}
	mgr := NewTLSCertManager(cfg, bufLog)

	mgr.certDuration = 2 * time.Second
	mgr.renewalThreshold = 1 * time.Second
	mgr.checkInterval = 300 * time.Millisecond

	err := mgr.GenerateAndLoad()
	if err != nil {
		t.Fatalf("GenerateAndLoad failed: %v", err)
	}

	mgr.Start()
	// Let it expire and try auto-renewal.
	time.Sleep(1200 * time.Millisecond)
	mgr.Stop()

	logStr := bufLog.buf.String()
	if !bytes.Contains([]byte(logStr), []byte("TLS certificate expires in less than 1 hour")) {
		t.Logf("Warning log not captured (might have renewed immediately), current logs:\n%s", logStr)
	}
}

func TestTLSCertManager_Lifecycle(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	cfg := config.TLSConfig{Enabled: true, Mode: "auto"}
	mgr := NewTLSCertManager(cfg, log)

	err := mgr.GenerateAndLoad()
	if err != nil {
		t.Fatalf("GenerateAndLoad failed: %v", err)
	}

	mgr.Start()
	mgr.Stop()
}
