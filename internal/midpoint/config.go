// Package midpoint is a thin client for the Evolveum midPoint REST API
// (/ws/rest/...). It reads credentials from the environment at runtime and
// never writes them to disk, logs, or tool output.
package midpoint

import (
	"fmt"
	"os"
	"strings"
)

// Environment variables consumed by ConfigFromEnv.
const (
	EnvURL         = "MIDPOINT_URL"
	EnvUsername    = "MIDPOINT_USERNAME"
	EnvPassword    = "MIDPOINT_PASSWORD"
	EnvInsecureTLS = "MIDPOINT_INSECURE_TLS"
)

// Config holds everything needed to reach a midPoint deployment.
//
// BaseURL is the deployment root (e.g. https://localhost:8443/midpoint); the
// client appends /ws/rest/... to it. Credentials are used for HTTP Basic auth,
// which is midPoint's native REST authentication.
type Config struct {
	BaseURL  string
	Username string
	Password string

	// InsecureTLS disables TLS certificate verification. It exists only so the
	// server can talk to midPoint dev instances that ship self-signed certs;
	// never enable it against a deployment you care about.
	InsecureTLS bool
}

// ConfigFromEnv builds a Config from the MIDPOINT_* environment variables,
// returning an error that names every missing required variable. The password
// value is never included in the error.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:     strings.TrimSpace(os.Getenv(EnvURL)),
		Username:    strings.TrimSpace(os.Getenv(EnvUsername)),
		Password:    os.Getenv(EnvPassword),
		InsecureTLS: strings.EqualFold(strings.TrimSpace(os.Getenv(EnvInsecureTLS)), "true"),
	}

	var missing []string
	if cfg.BaseURL == "" {
		missing = append(missing, EnvURL)
	}
	if cfg.Username == "" {
		missing = append(missing, EnvUsername)
	}
	if cfg.Password == "" {
		missing = append(missing, EnvPassword)
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}
