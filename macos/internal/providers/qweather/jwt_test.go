package qweather

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func writePKCS8Key(t *testing.T, value any) (string, ed25519.PublicKey) {
	t.Helper()
	var public ed25519.PublicKey
	if key, ok := value.(ed25519.PrivateKey); ok {
		public = key.Public().(ed25519.PublicKey)
	}
	data, err := x509.MarshalPKCS8PrivateKey(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, public
}

func decodeJWTPart(t *testing.T, part string, target any) {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func TestJWTSignerProducesVerifiableMinimalToken(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path, public := writePKCS8Key(t, private)
	signer, err := LoadJWTSigner(path, "credential-123", "project-456")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_710_000_000, 0)
	token, err := signer.Token(now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT segment count = %d", len(parts))
	}
	for _, part := range parts {
		if strings.Contains(part, "=") {
			t.Fatal("JWT Base64URL segments must not contain padding")
		}
	}
	var header map[string]any
	decodeJWTPart(t, parts[0], &header)
	if !reflect.DeepEqual(header, map[string]any{"alg": "EdDSA", "kid": "credential-123"}) {
		t.Fatalf("JWT header = %#v", header)
	}
	var payload map[string]any
	decodeJWTPart(t, parts[1], &payload)
	if !reflect.DeepEqual(payload, map[string]any{"sub": "project-456", "iat": float64(now.Unix() - 30), "exp": float64(now.Add(15 * time.Minute).Unix())}) {
		t.Fatalf("JWT payload = %#v", payload)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(public, []byte(parts[0]+"."+parts[1]), signature) {
		t.Fatal("JWT signature did not verify")
	}
}

func TestJWTSignerCachesAndRefreshesTwoMinutesEarly(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := writePKCS8Key(t, private)
	signer, err := LoadJWTSigner(path, "credential", "project")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_710_000_000, 0)
	first, _ := signer.Token(now)
	cached, _ := signer.Token(now.Add(12*time.Minute + 59*time.Second))
	refreshed, _ := signer.Token(now.Add(13 * time.Minute))
	if first != cached {
		t.Fatal("token refreshed before the two-minute threshold")
	}
	if first == refreshed {
		t.Fatal("token was not refreshed at the two-minute threshold")
	}
	invalidatedBefore, _ := signer.Token(now.Add(14 * time.Minute))
	signer.Invalidate()
	invalidatedAfter, _ := signer.Token(now.Add(14*time.Minute + time.Second))
	if invalidatedBefore == invalidatedAfter {
		t.Fatal("Invalidate did not force a new token")
	}
}

func TestJWTSignerConcurrentCallsReuseToken(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := writePKCS8Key(t, private)
	signer, err := LoadJWTSigner(path, "credential", "project")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_710_000_000, 0)
	results := make(chan string, 32)
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			token, tokenErr := signer.Token(now)
			if tokenErr != nil {
				t.Errorf("Token: %v", tokenErr)
				return
			}
			results <- token
		}()
	}
	group.Wait()
	close(results)
	var first string
	for token := range results {
		if first == "" {
			first = token
		}
		if token != first {
			t.Fatal("concurrent calls returned different cached tokens")
		}
	}
}

func TestJWTSignerRejectsInvalidAndNonEd25519PEM(t *testing.T) {
	invalidPath := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(invalidPath, []byte("not a PEM key"), 0o600); err != nil {
		t.Fatal(err)
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaPath, _ := writePKCS8Key(t, ecdsaKey)
	for _, testCase := range []struct {
		name string
		path string
		want string
	}{{"invalid PEM", invalidPath, "PKCS#8 PEM PRIVATE KEY"}, {"wrong algorithm", ecdsaPath, "not Ed25519"}} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, loadErr := LoadJWTSigner(testCase.path, "credential", "project"); loadErr == nil || !strings.Contains(loadErr.Error(), testCase.want) {
				t.Fatalf("LoadJWTSigner error = %v", loadErr)
			}
		})
	}
	if _, err := LoadJWTSigner(invalidPath, "", "project"); err == nil || !strings.Contains(err.Error(), "credential_id and project_id") {
		t.Fatalf("missing identity error = %v", err)
	}
}
