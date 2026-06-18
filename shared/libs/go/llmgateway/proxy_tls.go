package llmgateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"sync"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// TLSCertManager manages self-signed TLS certificate lifecycle.
type TLSCertManager struct {
	cfg        config.TLSConfig
	logger     logger.Logger
	mu         sync.RWMutex
	cert       *tls.Certificate
	caCertPEM  []byte
	caFilePath string
	expiresAt  time.Time
	stopCh     chan struct{}

	// For testing: override certificate duration and renewal threshold.
	certDuration     time.Duration // default: 24h
	renewalThreshold time.Duration // default: 1h before expiry
	checkInterval    time.Duration // default: 10m
}

// NewTLSCertManager creates a new TLS certificate manager.
func NewTLSCertManager(cfg config.TLSConfig, log logger.Logger) *TLSCertManager {
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	return &TLSCertManager{
		cfg:              cfg,
		logger:           log.WithComponent("tls-cert-manager"),
		stopCh:           make(chan struct{}),
		certDuration:     24 * time.Hour,
		renewalThreshold: 1 * time.Hour,
		checkInterval:    10 * time.Minute,
	}
}

// GenerateAndLoad generates a new self-signed certificate and loads it.
func (m *TLSCertManager) GenerateAndLoad() error {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("generate serial number: %w", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(m.certDuration)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: "tern-llmgp-local",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Local IP and Localhost SANs
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"))
	template.DNSNames = append(template.DNSNames, "localhost")

	// Extra SANs
	for _, san := range m.cfg.ExtraSANs {
		if ip := net.ParseIP(san); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, san)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privDer, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("marshal ec private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDer})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("load x509 key pair: %w", err)
	}

	m.mu.Lock()
	m.cert = &tlsCert
	m.caCertPEM = certPEM
	m.expiresAt = notAfter
	m.mu.Unlock()

	return nil
}

// GetCertificate returns the current certificate for tls.Config callback.
func (m *TLSCertManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil {
		return nil, fmt.Errorf("no TLS certificate available")
	}
	if time.Now().After(m.expiresAt) {
		m.logger.Error("TLS certificate has expired -- HTTPS connections will fail. Restart the server to generate a new certificate")
	}
	return m.cert, nil
}

// CACertFilePath returns the path to the CA cert PEM file.
func (m *TLSCertManager) CACertFilePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.caFilePath
}

// ExpiresAt returns the certificate expiration time.
func (m *TLSCertManager) ExpiresAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.expiresAt
}

// IsExpired returns true if the certificate has expired.
func (m *TLSCertManager) IsExpired() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Now().After(m.expiresAt)
}

// IsDegraded returns true if cert is expired or expiring within threshold.
func (m *TLSCertManager) IsDegraded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Until(m.expiresAt) < m.renewalThreshold
}

// Start begins the background certificate monitoring goroutine.
func (m *TLSCertManager) Start() {
	go func() {
		ticker := time.NewTicker(m.checkInterval)
		defer ticker.Stop()
		warnedAt2h := false
		warnedAt1h := false
		for {
			select {
			case <-ticker.C:
				m.mu.RLock()
				expiresAt := m.expiresAt
				m.mu.RUnlock()

				remaining := time.Until(expiresAt)
				if remaining <= 0 {
					m.logger.Error("TLS certificate has expired -- HTTPS connections will fail. Restart the server to generate a new certificate")
					m.tryAutoRenew()
				} else if remaining <= m.renewalThreshold {
					if !warnedAt1h {
						m.logger.Warn("TLS certificate expires in less than 1 hour -- if auto-renewal fails, restart the server to regenerate the certificate")
						warnedAt1h = true
					}
					m.tryAutoRenew()
				} else if remaining <= 2*time.Hour && !warnedAt2h {
					m.logger.Info("TLS certificate will expire in 2 hours, auto-renewal scheduled")
					warnedAt2h = true
				}
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background monitoring goroutine.
func (m *TLSCertManager) Stop() {
	close(m.stopCh)
}

// WriteCACertFile writes the CA cert PEM to a temporary file.
func (m *TLSCertManager) WriteCACertFile() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.caCertPEM) == 0 {
		return "", fmt.Errorf("no CA certificate generated")
	}

	// If already written, we can overwrite it or write to new one.
	// Overwriting is better if path is already set.
	var file *os.File
	var err error
	if m.caFilePath != "" {
		file, err = os.OpenFile(m.caFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			// fallback to a new temp file if open fails
			m.caFilePath = ""
		}
	}

	if m.caFilePath == "" {
		file, err = os.CreateTemp("", "tern-ca-*.pem")
		if err != nil {
			return "", fmt.Errorf("create temp CA file: %w", err)
		}
		m.caFilePath = file.Name()
	}

	defer file.Close()

	if _, err := file.Write(m.caCertPEM); err != nil {
		return "", fmt.Errorf("write CA certificate: %w", err)
	}

	return m.caFilePath, nil
}

func (m *TLSCertManager) tryAutoRenew() {
	if err := m.GenerateAndLoad(); err != nil {
		m.logger.Error("TLS certificate auto-renewal failed: " + err.Error() +
			" -- restart the server to restore HTTPS connectivity. " +
			"Workaround: restart the tern server process")
		return
	}
	if _, err := m.WriteCACertFile(); err != nil {
		m.logger.Warn("failed to update CA cert file: " + err.Error())
	}
	m.mu.RLock()
	expiresAt := m.expiresAt
	m.mu.RUnlock()
	m.logger.Info("TLS certificate auto-renewed successfully",
		"new_expiry", expiresAt.Format(time.RFC3339))
}
