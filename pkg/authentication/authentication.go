// Package authentication verifies the JWT bearer tokens that clients send
// in the "authorization" gRPC metadata key or the Authorization HTTP header.
package authentication

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/entities"
)

type Authenticator struct {
	method    jwt.SigningMethod
	verifyKey any
	parser    *jwt.Parser
}

// New returns nil when authentication is disabled, so callers can skip the
// check entirely.
func New(cfg *config.AuthenticationConfig) (*Authenticator, error) {
	if cfg == nil || !cfg.Enable {
		return nil, nil
	}
	return newAuthenticator(cfg.JwtConfig)
}

func newAuthenticator(cfg *config.JWTAuthenticationConfig) (*Authenticator, error) {
	if cfg == nil {
		return nil, entities.ErrAuthInvalidNoKey
	}
	method := signingMethod(cfg.SignatureType)
	if method == nil {
		return nil, fmt.Errorf("unsupported jwt signature type %q", cfg.SignatureType)
	}
	keyData, err := loadKey(cfg)
	if err != nil {
		return nil, err
	}
	var verifyKey any
	switch method.(type) {
	case *jwt.SigningMethodHMAC:
		verifyKey = keyData
	case *jwt.SigningMethodRSA, *jwt.SigningMethodRSAPSS:
		verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(keyData)
	case *jwt.SigningMethodECDSA:
		verifyKey, err = jwt.ParseECPublicKeyFromPEM(keyData)
	case *jwt.SigningMethodEd25519:
		verifyKey, err = jwt.ParseEdPublicKeyFromPEM(keyData)
	}
	if err != nil {
		return nil, fmt.Errorf("%w%s", entities.ErrAuthInvalidKeyFile, err)
	}
	return &Authenticator{
		method:    method,
		verifyKey: verifyKey,
		parser:    jwt.NewParser(jwt.WithValidMethods([]string{method.Alg()})),
	}, nil
}

// Verify checks a token taken from a request. A "Bearer " prefix is optional,
// because the Go SDK's REST transport adds one and the gRPC SDKs do not.
func (a *Authenticator) Verify(token string) error {
	token = strings.TrimSpace(token)
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return fmt.Errorf("%wno token provided", entities.ErrAuthInvalidAuthToken)
	}
	_, err := a.parser.Parse(token, func(*jwt.Token) (any, error) { return a.verifyKey, nil })
	if err != nil {
		return fmt.Errorf("%w%s", entities.ErrAuthInvalidAuthToken, err)
	}
	return nil
}

// Sign creates a token that the configured key verifies. HMAC tokens are
// signed with the configured secret. The other methods need the private key
// matching the configured public key, as PEM.
func Sign(cfg *config.JWTAuthenticationConfig, privateKey []byte, subject string, ttl time.Duration) (string, error) {
	auth, err := newAuthenticator(cfg)
	if err != nil {
		return "", err
	}
	var signKey any
	switch auth.method.(type) {
	case *jwt.SigningMethodHMAC:
		signKey = auth.verifyKey
	case *jwt.SigningMethodRSA, *jwt.SigningMethodRSAPSS:
		signKey, err = jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	case *jwt.SigningMethodECDSA:
		signKey, err = jwt.ParseECPrivateKeyFromPEM(privateKey)
	case *jwt.SigningMethodEd25519:
		signKey, err = jwt.ParseEdPrivateKeyFromPEM(privateKey)
	}
	if err != nil {
		return "", fmt.Errorf("invalid private key for %s: %w", auth.method.Alg(), err)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{Subject: subject, IssuedAt: jwt.NewNumericDate(now)}
	if ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}
	token, err := jwt.NewWithClaims(auth.method, claims).SignedString(signKey)
	if err != nil {
		return "", err
	}
	// Check the result against the configured key, so a private key that
	// doesn't match it fails here instead of at the server.
	if err := auth.Verify(token); err != nil {
		return "", fmt.Errorf("the private key does not match the configured key: %w", err)
	}
	return token, nil
}

// UsesHMAC reports whether tokens are signed with the configured secret
// rather than a separate private key.
func UsesHMAC(cfg *config.JWTAuthenticationConfig) bool {
	_, ok := signingMethod(cfg.SignatureType).(*jwt.SigningMethodHMAC)
	return ok
}

func signingMethod(name string) jwt.SigningMethod {
	for _, alg := range config.SupportedJWTSignatureTypes {
		if strings.EqualFold(alg, name) {
			return jwt.GetSigningMethod(alg)
		}
	}
	return nil
}

func loadKey(cfg *config.JWTAuthenticationConfig) ([]byte, error) {
	if cfg.Key != "" {
		return []byte(cfg.Key), nil
	}
	if cfg.FilePath == "" {
		return nil, entities.ErrAuthInvalidNoKey
	}
	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		return nil, fmt.Errorf("%w%s", entities.ErrAuthInvalidKeyFile, err)
	}
	// Editors add a trailing newline to secret files; it isn't part of the key.
	data = []byte(strings.TrimRight(string(data), "\r\n"))
	if len(data) == 0 {
		return nil, entities.ErrAuthInvalidNoKey
	}
	return data, nil
}
