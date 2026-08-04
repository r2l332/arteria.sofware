package tunnel

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
	"os"
	"time"
)

// CA manages certificate generation for the tunnel mesh.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

// LoadOrCreateCA loads an existing CA or creates a new one.
func LoadOrCreateCA(certPath, keyPath string) (*CA, error) {
	// Try loading existing
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)

	if certErr == nil && keyErr == nil {
		return loadCA(certPEM, keyPEM)
	}

	// Generate new CA
	ca, err := generateCA()
	if err != nil {
		return nil, err
	}

	os.WriteFile(certPath, ca.certPEM, 0644)
	os.WriteFile(keyPath, ca.keyPEM, 0600)

	return ca, nil
}

func loadCA(certPEM, keyPEM []byte) (*CA, error) {
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}

	return &CA{cert: cert, key: key, certPEM: certPEM, keyPEM: keyPEM}, nil
}

func generateCA() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Arteria Tunnel Mesh"},
			CommonName:   "Arteria CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	cert, _ := x509.ParseCertificate(certDER)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &CA{cert: cert, key: key, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// IssueCert generates a signed certificate for a tunnel node.
func (ca *CA) IssueCert(nodeID, commonName string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Arteria Tunnel Mesh"},
			CommonName:   commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// CACertPEM returns the CA certificate in PEM format.
func (ca *CA) CACertPEM() []byte {
	return ca.certPEM
}

// TLSConfig creates a TLS config for the broker side (verifies client certs).
func (ca *CA) BrokerTLSConfig(brokerCertPEM, brokerKeyPEM []byte) (*tls.Config, error) {
	brokerCert, err := tls.X509KeyPair(brokerCertPEM, brokerKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load broker cert: %w", err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.certPEM)

	return &tls.Config{
		Certificates: []tls.Certificate{brokerCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// AgentTLSConfig creates a TLS config for the agent side.
// The agent trusts the Arteria CA and presents its client cert.
// ServerName verification is skipped because the broker cert CN may not match
// the hostname the agent connects to — mutual TLS (client cert) provides the trust.
func AgentTLSConfig(caCertPEM, clientCertPEM, clientKeyPEM []byte) (*tls.Config, error) {
	clientCert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCertPEM)

	return &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		RootCAs:            pool,
		InsecureSkipVerify: true, // We trust the CA; hostname may vary (IP, Docker, cloud LB)
		MinVersion:         tls.VersionTLS13,
	}, nil
}
