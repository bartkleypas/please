package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CertBundle contains the paths to the generated certificates and private keys.
type CertBundle struct {
	CACertPath     string
	CAKeyPath      string
	ServerCertPath string
	ServerKeyPath  string
}

// Generate20YearCerts generates a self-signed Root CA and a Server Leaf Certificate
// valid for 20 years (7,300 days) with Subject Alternative Names (SANs).
func Generate20YearCerts(outDir string, hosts []string) (*CertBundle, error) {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	caCertPath := filepath.Join(outDir, "ca.crt")
	caKeyPath := filepath.Join(outDir, "ca.key")
	serverCertPath := filepath.Join(outDir, "server.crt")
	serverKeyPath := filepath.Join(outDir, "server.key")

	// 1. Generate Root CA Private Key (ECDSA P-256)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	caSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA serial number: %w", err)
	}

	// 20-year duration (7300 days)
	notBefore := time.Now().Add(-1 * time.Hour)
	notAfter := notBefore.Add(7300 * 24 * time.Hour)

	caTemplate := &x509.Certificate{
		SerialNumber: caSerial,
		Subject: pkix.Name{
			Organization: []string{"Please Internal Authority"},
			CommonName:   "Please-Internal-Root-CA",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	// Save CA private key
	caKeyBytes, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CA private key: %w", err)
	}
	if err := savePEM(caKeyPath, "EC PRIVATE KEY", caKeyBytes, 0600); err != nil {
		return nil, err
	}

	// Save CA certificate
	if err := savePEM(caCertPath, "CERTIFICATE", caBytes, 0644); err != nil {
		return nil, err
	}

	// 2. Generate Server Leaf Private Key (ECDSA P-256)
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server private key: %w", err)
	}

	serverSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate server serial number: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject: pkix.Name{
			Organization: []string{"Please Local Server"},
			CommonName:   "please.local",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add SANs
	defaultHosts := []string{"localhost", "127.0.0.1", "::1", "please.local"}
	allHosts := append(defaultHosts, hosts...)
	seen := make(map[string]bool)

	for _, h := range allHosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true

		if ip := net.ParseIP(h); ip != nil {
			serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, ip)
		} else {
			serverTemplate.DNSNames = append(serverTemplate.DNSNames, h)
		}
	}

	serverBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign server certificate: %w", err)
	}

	// Save Server private key
	serverKeyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server private key: %w", err)
	}
	if err := savePEM(serverKeyPath, "EC PRIVATE KEY", serverKeyBytes, 0600); err != nil {
		return nil, err
	}

	// Save Server certificate
	if err := savePEM(serverCertPath, "CERTIFICATE", serverBytes, 0644); err != nil {
		return nil, err
	}

	return &CertBundle{
		CACertPath:     caCertPath,
		CAKeyPath:      caKeyPath,
		ServerCertPath: serverCertPath,
		ServerKeyPath:  serverKeyPath,
	}, nil
}

func savePEM(filename, blockType string, bytes []byte, perm os.FileMode) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", filename, err)
	}
	defer file.Close()

	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: bytes}); err != nil {
		return fmt.Errorf("failed to encode PEM block into %s: %w", filename, err)
	}
	return nil
}
