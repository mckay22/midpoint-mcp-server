package midpoint

import "testing"

func TestConfigOIDC(t *testing.T) {
	base := func() {
		t.Setenv(EnvURL, "https://mp.example.com/midpoint")
		t.Setenv(EnvUsername, "svc")
		t.Setenv(EnvPassword, "secret")
	}

	t.Run("neither is personal mode", func(t *testing.T) {
		base()
		t.Setenv(EnvOIDCIssuer, "")
		t.Setenv(EnvOIDCAudience, "")
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ResourceServerMode() {
			t.Error("ResourceServerMode true with no OIDC config")
		}
	})

	t.Run("both is resource-server mode", func(t *testing.T) {
		base()
		t.Setenv(EnvOIDCIssuer, "https://kc.example.com/realms/x")
		t.Setenv(EnvOIDCAudience, "midpoint-mcp")
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.ResourceServerMode() {
			t.Error("ResourceServerMode false with full OIDC config")
		}
	})

	t.Run("issuer without audience is rejected", func(t *testing.T) {
		base()
		t.Setenv(EnvOIDCIssuer, "https://kc.example.com/realms/x")
		t.Setenv(EnvOIDCAudience, "")
		if _, err := ConfigFromEnv(); err == nil {
			t.Error("expected error when only the issuer is set")
		}
	})

	t.Run("audience without issuer is rejected", func(t *testing.T) {
		base()
		t.Setenv(EnvOIDCIssuer, "")
		t.Setenv(EnvOIDCAudience, "midpoint-mcp")
		if _, err := ConfigFromEnv(); err == nil {
			t.Error("expected error when only the audience is set")
		}
	})

	t.Run("custom correlation claim/attribute parsed", func(t *testing.T) {
		base()
		t.Setenv(EnvOIDCIssuer, "https://kc.example.com/realms/x")
		t.Setenv(EnvOIDCAudience, "midpoint-mcp")
		t.Setenv(EnvOIDCCorrelationClaim, "email")
		t.Setenv(EnvOIDCCorrelationAttribute, "emailAddress")
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.OIDCCorrelationClaim != "email" || cfg.OIDCCorrelationAttribute != "emailAddress" {
			t.Errorf("correlation cfg = %q / %q", cfg.OIDCCorrelationClaim, cfg.OIDCCorrelationAttribute)
		}
	})

	t.Run("invalid correlation attribute is rejected", func(t *testing.T) {
		base()
		t.Setenv(EnvOIDCIssuer, "https://kc.example.com/realms/x")
		t.Setenv(EnvOIDCAudience, "midpoint-mcp")
		t.Setenv(EnvOIDCCorrelationAttribute, `name = "x" or 1=1`)
		if _, err := ConfigFromEnv(); err == nil {
			t.Error("expected error for an injection-shaped correlation attribute")
		}
	})
}

func TestValidCorrelationAttribute(t *testing.T) {
	ok := []string{"name", "emailAddress", "employeeNumber", "extension/badgeId", "a"}
	bad := []string{"", "1name", "/name", "name/", "a//b", "name = x", `name"`, "na me", "name;drop"}
	for _, s := range ok {
		if !ValidCorrelationAttribute(s) {
			t.Errorf("ValidCorrelationAttribute(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidCorrelationAttribute(s) {
			t.Errorf("ValidCorrelationAttribute(%q) = true, want false", s)
		}
	}
}
