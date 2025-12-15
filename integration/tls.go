// Copyright 2025 CruxStack
// SPDX-License-Identifier: MIT

//go:build integration

package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"
)

// sharedCrypto holds cryptographic materials generated once at test startup.
var sharedCrypto struct {
	once      sync.Once
	tlsCert   tls.Certificate
	certPool  *x509.CertPool
	rsaKeyPEM string
	err       error
}

// initSharedCrypto generates all cryptographic materials once at test startup.
func initSharedCrypto() {
	sharedCrypto.once.Do(func() {
		// Generate TLS certificate
		sharedCrypto.tlsCert, sharedCrypto.certPool, sharedCrypto.err = generateSelfSignedCert()
		if sharedCrypto.err != nil {
			return
		}

		// Generate RSA key for GitHub App simulation
		sharedCrypto.rsaKeyPEM, sharedCrypto.err = generateRSAKeyPEM()
	})
}

// getSharedTLSCert returns the shared TLS certificate.
func getSharedTLSCert() (tls.Certificate, *x509.CertPool, error) {
	initSharedCrypto()
	return sharedCrypto.tlsCert, sharedCrypto.certPool, sharedCrypto.err
}

// getSharedRSAKeyPEM returns the shared RSA private key PEM.
func getSharedRSAKeyPEM() (string, error) {
	initSharedCrypto()
	return sharedCrypto.rsaKeyPEM, sharedCrypto.err
}

// generateSelfSignedCert creates a self-signed TLS certificate for localhost.
func generateSelfSignedCert() (tls.Certificate, *x509.CertPool, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("generate private key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Integration Tests"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("create key pair: %w", err)
	}

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(certPEM)

	return tlsCert, certPool, nil
}

// generateRSAKeyPEM generates a fresh RSA private key in PEM format.
func generateRSAKeyPEM() (string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", fmt.Errorf("generate RSA key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return string(keyPEM), nil
}
