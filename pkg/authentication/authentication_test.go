package authentication

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/entities"
	"github.com/stretchr/testify/require"
)

const secret = "test-secret-with-enough-length-for-hs256"

func hmacConfig() *config.JWTAuthenticationConfig {
	return &config.JWTAuthenticationConfig{Key: secret, SignatureType: "HS256"}
}

func pemBlock(t *testing.T, typ string, der []byte, err error) []byte {
	t.Helper()
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func TestNew_Disabled(t *testing.T) {
	auth, err := New(&config.AuthenticationConfig{Enable: false})
	require.NoError(t, err)
	require.Nil(t, auth)
	auth, err = New(nil)
	require.NoError(t, err)
	require.Nil(t, auth)
}

func TestNew_BadConfig(t *testing.T) {
	_, err := newAuthenticator(&config.JWTAuthenticationConfig{Key: secret, SignatureType: "none"})
	require.Error(t, err)
	_, err = newAuthenticator(&config.JWTAuthenticationConfig{SignatureType: "HS256"})
	require.ErrorIs(t, err, entities.ErrAuthInvalidNoKey)
	_, err = newAuthenticator(&config.JWTAuthenticationConfig{Key: "not a pem key", SignatureType: "RS256"})
	require.ErrorIs(t, err, entities.ErrAuthInvalidKeyFile)
	_, err = newAuthenticator(&config.JWTAuthenticationConfig{FilePath: filepath.Join(t.TempDir(), "missing"), SignatureType: "HS256"})
	require.ErrorIs(t, err, entities.ErrAuthInvalidKeyFile)
}

func TestVerify_HMAC(t *testing.T) {
	cfg := hmacConfig()
	auth, err := newAuthenticator(cfg)
	require.NoError(t, err)

	valid, err := Sign(cfg, nil, "orders", time.Hour)
	require.NoError(t, err)
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).SignedString([]byte(secret))
	require.NoError(t, err)
	otherSecret, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{}).SignedString([]byte("some-other-secret"))
	require.NoError(t, err)
	// Same secret, different algorithm: rejected because only the configured one is accepted.
	otherAlg, err := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.RegisteredClaims{}).SignedString([]byte(secret))
	require.NoError(t, err)
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"valid", valid, false},
		{"bearer prefix", "Bearer " + valid, false},
		{"lowercase bearer prefix", "bearer " + valid, false},
		{"empty", "", true},
		{"bearer only", "Bearer ", true},
		{"garbage", "not-a-jwt", true},
		{"expired", expired, true},
		{"other secret", otherSecret, true},
		{"other algorithm", otherAlg, true},
		{"alg none", unsigned, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.Verify(tt.token)
			if tt.wantErr {
				require.True(t, errors.Is(err, entities.ErrAuthInvalidAuthToken), "got %v", err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerify_KeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(path, []byte(secret+"\n"), 0o600))
	fileCfg := &config.JWTAuthenticationConfig{FilePath: path, SignatureType: "HS256"}
	auth, err := newAuthenticator(fileCfg)
	require.NoError(t, err)
	// A token signed with the inline secret verifies against the file, so the
	// trailing newline is not part of the key.
	token, err := Sign(hmacConfig(), nil, "", 0)
	require.NoError(t, err)
	require.NoError(t, auth.Verify(token))
}

func TestSign_Asymmetric(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherRSA, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pub := func(k any) []byte {
		der, err := x509.MarshalPKIXPublicKey(k)
		return pemBlock(t, "PUBLIC KEY", der, err)
	}
	priv := func(k any) []byte {
		der, err := x509.MarshalPKCS8PrivateKey(k)
		return pemBlock(t, "PRIVATE KEY", der, err)
	}

	tests := []struct {
		alg        string
		publicKey  []byte
		privateKey []byte
	}{
		{"RS256", pub(&rsaKey.PublicKey), priv(rsaKey)},
		{"PS384", pub(&rsaKey.PublicKey), priv(rsaKey)},
		{"ES256", pub(&ecKey.PublicKey), priv(ecKey)},
		{"EdDSA", pub(edPub), priv(edPriv)},
	}
	for _, tt := range tests {
		t.Run(tt.alg, func(t *testing.T) {
			cfg := &config.JWTAuthenticationConfig{Key: string(tt.publicKey), SignatureType: tt.alg}
			token, err := Sign(cfg, tt.privateKey, "svc", time.Hour)
			require.NoError(t, err)
			auth, err := newAuthenticator(cfg)
			require.NoError(t, err)
			require.NoError(t, auth.Verify(token))
		})
	}

	t.Run("mismatched private key", func(t *testing.T) {
		cfg := &config.JWTAuthenticationConfig{Key: string(pub(&rsaKey.PublicKey)), SignatureType: "RS256"}
		_, err := Sign(cfg, priv(otherRSA), "svc", 0)
		require.ErrorContains(t, err, "does not match")
	})
	t.Run("missing private key", func(t *testing.T) {
		cfg := &config.JWTAuthenticationConfig{Key: string(pub(&rsaKey.PublicKey)), SignatureType: "RS256"}
		_, err := Sign(cfg, nil, "svc", 0)
		require.Error(t, err)
	})
}

func TestUsesHMAC(t *testing.T) {
	require.True(t, UsesHMAC(&config.JWTAuthenticationConfig{SignatureType: "hs512"}))
	require.False(t, UsesHMAC(&config.JWTAuthenticationConfig{SignatureType: "RS256"}))
	require.False(t, UsesHMAC(&config.JWTAuthenticationConfig{SignatureType: "bogus"}))
}
