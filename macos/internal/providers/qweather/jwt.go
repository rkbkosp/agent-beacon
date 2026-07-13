package qweather

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

type jwtHeader struct {
	Algorithm    string `json:"alg"`
	CredentialID string `json:"kid"`
}

type jwtPayload struct {
	ProjectID string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type JWTSigner struct {
	credentialID string
	projectID    string
	privateKey   ed25519.PrivateKey

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

func LoadJWTSigner(privateKeyPath, credentialID, projectID string) (*JWTSigner, error) {
	if credentialID == "" || projectID == "" {
		return nil, errors.New("qweather credential_id and project_id are required")
	}
	data, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read qweather private key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		return nil, errors.New("qweather private key must be an unencrypted PKCS#8 PEM PRIVATE KEY")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse qweather PKCS#8 private key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("qweather private key is not Ed25519")
	}
	return &JWTSigner{credentialID: credentialID, projectID: projectID, privateKey: privateKey}, nil
}

func (signer *JWTSigner) Token(now time.Time) (string, error) {
	signer.mu.Lock()
	defer signer.mu.Unlock()
	if signer.cached != "" && now.Before(signer.expiresAt.Add(-2*time.Minute)) {
		return signer.cached, nil
	}
	headerJSON, err := json.Marshal(jwtHeader{Algorithm: "EdDSA", CredentialID: signer.credentialID})
	if err != nil {
		return "", fmt.Errorf("encode qweather JWT header: %w", err)
	}
	expiresAt := now.Add(15 * time.Minute)
	payloadJSON, err := json.Marshal(jwtPayload{ProjectID: signer.projectID, IssuedAt: now.Unix() - 30, ExpiresAt: expiresAt.Unix()})
	if err != nil {
		return "", fmt.Errorf("encode qweather JWT payload: %w", err)
	}
	encoding := base64.RawURLEncoding
	header := encoding.EncodeToString(headerJSON)
	payload := encoding.EncodeToString(payloadJSON)
	signingInput := header + "." + payload
	signature := ed25519.Sign(signer.privateKey, []byte(signingInput))
	token := signingInput + "." + encoding.EncodeToString(signature)
	signer.cached = token
	signer.expiresAt = expiresAt
	return token, nil
}

func (signer *JWTSigner) Invalidate() {
	signer.mu.Lock()
	signer.cached = ""
	signer.expiresAt = time.Time{}
	signer.mu.Unlock()
}
