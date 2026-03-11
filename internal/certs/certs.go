package certs

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
	"time"
)

// EnsureCertificates checks for existing TLS certificates in atlasDir,
// regenerates them if expired or corrupt, and creates new ones if missing.
// Returns paths to cert.pem and key.pem.
func EnsureCertificates(atlasDir string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(atlasDir, "cert.pem")
	keyPath = filepath.Join(atlasDir, "key.pem")

	// Check if both files exist
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)

	if certExists && keyExists {
		// Validate existing cert is not expired or corrupt
		if valid, _ := isCertValid(certPath); valid {
			return certPath, keyPath, nil
		}
		// Expired or corrupt — regenerate
	}

	// Generate new self-signed certificate
	if err := generateCert(certPath, keyPath); err != nil {
		return "", "", fmt.Errorf("failed to generate TLS certificate: %w", err)
	}

	return certPath, keyPath, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isCertValid(certPath string) (bool, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return false, fmt.Errorf("failed to decode PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}

	// Valid if not yet expired (with 7-day buffer)
	return time.Now().Add(7 * 24 * time.Hour).Before(cert.NotAfter), nil
}

func generateCert(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	hostname, _ := os.Hostname()

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Atlas"},
			CommonName:   "Atlas Self-Signed",
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	if hostname != "" {
		template.DNSNames = append(template.DNSNames, hostname)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write cert.pem (0644)
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write cert PEM: %w", err)
	}

	// Write key.pem (0600)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		return fmt.Errorf("failed to write key PEM: %w", err)
	}

	return nil
}
