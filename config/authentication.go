package config

import "strings"

var validAuthTypes = map[string]string{
	"local": "local",
}

// SupportedJWTSignatureTypes lists the signatureType values the server can
// verify. HMAC types take a shared secret as the key; the others take a PEM
// public key.
var SupportedJWTSignatureTypes = []string{
	"HS256", "HS384", "HS512",
	"RS256", "RS384", "RS512",
	"PS256", "PS384", "PS512",
	"ES256", "ES384", "ES512",
	"EdDSA",
}

type JWTAuthenticationConfig struct {
	Key           string `json:"key"`
	FilePath      string `json:"filePath"`
	SignatureType string `json:"signatureType"`
}

func (jc *JWTAuthenticationConfig) Validate() error {
	if validateParameters(jc.Key, jc.FilePath, jc.SignatureType) {
		return NewConfigurationError("missing jwt configuration parameters")
	}
	if jc.SignatureType == "" {
		return NewConfigurationError("bad jwt configuration: missing jwt signature type")
	}
	supported := false
	for _, t := range SupportedJWTSignatureTypes {
		if strings.EqualFold(t, jc.SignatureType) {
			supported = true
		}
	}
	if !supported {
		return NewConfigurationErrorf("bad jwt configuration: unsupported signature type %q, use one of %s", jc.SignatureType, strings.Join(SupportedJWTSignatureTypes, ", "))
	}
	if jc.Key == "" && jc.FilePath == "" {
		return NewConfigurationError("bad jwt configuration: no verification key provided")
	}
	if jc.Key == "" && jc.FilePath != "" {
		if err := validateFileName(jc.FilePath); err != nil {
			return NewConfigurationErrorf("bad jwt configuration: %s", err.Error())
		}
	}

	return nil
}

type AuthenticationConfig struct {
	Enable    bool                     `json:"enable"`
	JwtConfig *JWTAuthenticationConfig `json:"jwtConfig"`
	Type      string                   `json:"type"`
	Config    string                   `json:"config"`
}

func defaultAuthenticationConfig() *AuthenticationConfig {
	bindViperEnv(
		"Authentication.Enable",
		"Authentication.JwtConfig.Key",
		"Authentication.JwtConfig.FilePath",
		"Authentication.JwtConfig.SignatureType",
		"Authentication.Type",
		"Authentication.Config",
	)

	return &AuthenticationConfig{
		Enable: false,
		JwtConfig: &JWTAuthenticationConfig{
			Key:           "",
			FilePath:      "",
			SignatureType: "",
		},
		Type:   "",
		Config: "",
	}
}

func (c *AuthenticationConfig) Validate() error {
	if !c.Enable {
		return nil
	}
	if c.JwtConfig != nil {
		return c.JwtConfig.Validate()
	} else {
		_, ok := validAuthTypes[strings.ToLower(c.Type)]
		if !ok {
			return NewConfigurationError("invalid authentication type")
		}
		if c.Config == "" {
			return NewConfigurationError("invalid authentication config")
		}
		return nil
	}
}
