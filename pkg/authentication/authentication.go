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
	"github.com/kubemq-io/kubemq-community/pkg/filewatch"
	"github.com/kubemq-io/kubemq-community/pkg/logging"
	"github.com/nats-io/nuid"
)

// revocationCheckInterval is how often the revoked tokens file is checked
// for changes, which bounds how long a newly revoked token keeps working.
const revocationCheckInterval = 5 * time.Second

type Authenticator struct {
	method    jwt.SigningMethod
	verifyKey any
	parser    *jwt.Parser
	revoked   *filewatch.File[map[string]bool]
}

// New returns nil when authentication is disabled, so callers can skip the
// check entirely.
func New(cfg *config.AuthenticationConfig) (*Authenticator, error) {
	if cfg == nil || !cfg.Enable {
		return nil, nil
	}
	a, err := newAuthenticator(cfg.JwtConfig)
	if err != nil {
		return nil, err
	}
	if cfg.RevokedTokensFile != "" {
		logger := logging.GetLogFactory().NewLogger("authentication")
		a.revoked, err = filewatch.New(cfg.RevokedTokensFile, revocationCheckInterval, parseRevokedTokens, func(err error) {
			logger.Errorf("error reloading revoked tokens, keeping the previous list: %s", err.Error())
		})
		if err != nil {
			return nil, fmt.Errorf("%wrevoked tokens file: %s", entities.ErrAuthInvalidKeyFile, err)
		}
	}
	return a, nil
}

// parseRevokedTokens reads one token ID per line. Blank lines and text after
// a # are ignored, so each ID can carry a note on why it was revoked.
func parseRevokedTokens(data []byte) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if id := strings.TrimSpace(line); id != "" {
			ids[id] = true
		}
	}
	return ids, nil
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
	_, err := a.Claims(token)
	return err
}

// Claims verifies a token like Verify and returns its registered claims.
func (a *Authenticator) Claims(token string) (*jwt.RegisteredClaims, error) {
	token = BareToken(token)
	if token == "" {
		return nil, fmt.Errorf("%wno token provided", entities.ErrAuthInvalidAuthToken)
	}
	claims := &jwt.RegisteredClaims{}
	_, err := a.parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return a.verifyKey, nil })
	if err != nil {
		return nil, fmt.Errorf("%w%s", entities.ErrAuthInvalidAuthToken, err)
	}
	if a.revoked != nil && claims.ID != "" && a.revoked.Get()[claims.ID] {
		return nil, fmt.Errorf("%wtoken %s is revoked", entities.ErrAuthInvalidAuthToken, claims.ID)
	}
	return claims, nil
}

// Sign creates a token that the configured key verifies and returns it with
// its ID (the jti claim), which is what a revocation list names. HMAC tokens
// are signed with the configured secret. The other methods need the private
// key matching the configured public key, as PEM.
func Sign(cfg *config.JWTAuthenticationConfig, privateKey []byte, subject string, ttl time.Duration) (token, id string, err error) {
	auth, err := newAuthenticator(cfg)
	if err != nil {
		return "", "", err
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
		return "", "", fmt.Errorf("invalid private key for %s: %w", auth.method.Alg(), err)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{ID: nuid.Next(), Subject: subject, IssuedAt: jwt.NewNumericDate(now)}
	if ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}
	token, err = jwt.NewWithClaims(auth.method, claims).SignedString(signKey)
	if err != nil {
		return "", "", err
	}
	// Check the result against the configured key, so a private key that
	// doesn't match it fails here instead of at the server.
	if err := auth.Verify(token); err != nil {
		return "", "", fmt.Errorf("the private key does not match the configured key: %w", err)
	}
	return token, claims.ID, nil
}

// BareToken trims whitespace and an optional "Bearer " prefix.
func BareToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	return token
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
