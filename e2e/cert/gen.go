package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"time"
)

func main() {
	// Generate CA private key
	caPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	// Create a CA certificate template
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2024), // Change this to a unique serial number
		Subject: pkix.Name{
			CommonName:   "PBR Test CA",
			Organization: []string{"Great Lion Technologies"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	// Self-sign the certificate
	caCertificateBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		panic(err)
	}

	// Save CA private key to file (e.g., ca.key)
	caPrivateKeyFile, err := os.Create("ca.key")
	if err != nil {
		panic(err)
	}
	defer caPrivateKeyFile.Close()

	// Write private key to file
	caPrivateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caPrivateKey)}
	if err := pem.Encode(caPrivateKeyFile, caPrivateKeyPEM); err != nil {
		panic(err)
	}

	// Save CA certificate to file (e.g., ca.crt)
	caCertFile, err := os.Create("ca.crt")
	if err != nil {
		panic(err)
	}
	defer caCertFile.Close()

	// Write certificate to file
	caCertPEM := &pem.Block{Type: "CERTIFICATE", Bytes: caCertificateBytes}
	if err := pem.Encode(caCertFile, caCertPEM); err != nil {
		panic(err)
	}

	// Generate private key for new certificate
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	// Create certificate template with SANs
	template := x509.Certificate{
		SerialNumber: big.NewInt(2025), // Change this to a unique serial number
		Subject: pkix.Name{
			Organization: []string{"Great Lion Technologies"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0), // Valid for 1 year
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	// Add SANs to the certificate template
	template.DNSNames = []string{"pbr-example.greatlion.tech"}

	// Generate certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &caTemplate, &key.PublicKey, caPrivateKey)
	if err != nil {
		panic(err)
	}

	// Save private key to file (e.g., server.key)
	keyOut, err := os.Create("server.key")
	if err != nil {
		panic(err)
	}
	defer keyOut.Close()
	keyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if err := pem.Encode(keyOut, keyPEM); err != nil {
		panic(err)
	}

	// Save certificate to file (e.g., server.crt)
	certOut, err := os.Create("server.crt")
	if err != nil {
		panic(err)
	}
	defer certOut.Close()
	certPEM := &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}
	if err := pem.Encode(certOut, certPEM); err != nil {
		panic(err)
	}
}
