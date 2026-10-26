package token

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/pkg/utils"
	"github.com/spf13/cobra"
)

type Options struct {
	cfg        *config.Config
	subject    string
	ttl        time.Duration
	signingKey string
}

var tokenExamples = `
	# token signed with the HMAC secret in authentication.jwtConfig, valid for 30 days
	kubemq token --subject orders-service --ttl 720h

	# RS/PS/ES/EdDSA: sign with the private key matching the configured public key
	kubemq token --subject orders-service --signing-key ./jwt-private.pem
`

func NewCmdToken(ctx context.Context, cfg *config.Config) *cobra.Command {
	o := &Options{
		cfg: cfg,
	}
	cmd := &cobra.Command{
		Use:     "token",
		Short:   "Create a JWT that clients can use when the server has authentication enabled",
		Example: tokenExamples,
		Run: func(cmd *cobra.Command, args []string) {
			utils.CheckErr(o.Validate())
			utils.CheckErr(o.Run(ctx))
		},
	}
	cmd.Flags().StringVarP(&o.subject, "subject", "s", "", "subject (sub claim) naming who the token is for")
	cmd.Flags().DurationVarP(&o.ttl, "ttl", "t", 0, "how long the token is valid, e.g. 24h; 0 means it never expires")
	cmd.Flags().StringVarP(&o.signingKey, "signing-key", "k", "", "PEM private key file, required for RS/PS/ES/EdDSA signature types")
	return cmd
}

func (o *Options) Validate() error {
	if o.ttl < 0 {
		return fmt.Errorf("--ttl cannot be negative")
	}
	jwtCfg := o.cfg.Authentication.JwtConfig
	if jwtCfg == nil || jwtCfg.SignatureType == "" {
		return fmt.Errorf("no JWT configuration found: set authentication.jwtConfig in config.yaml or the AUTHENTICATION_JWT_CONFIG_* environment variables")
	}
	if !authentication.UsesHMAC(jwtCfg) && o.signingKey == "" {
		return fmt.Errorf("signature type %s needs --signing-key with the private key", jwtCfg.SignatureType)
	}
	return nil
}

func (o *Options) Run(ctx context.Context) error {
	var privateKey []byte
	if o.signingKey != "" {
		data, err := os.ReadFile(o.signingKey)
		if err != nil {
			return err
		}
		privateKey = data
	}
	token, err := authentication.Sign(o.cfg.Authentication.JwtConfig, privateKey, o.subject, o.ttl)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}
