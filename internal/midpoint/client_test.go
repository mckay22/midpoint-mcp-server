package midpoint

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSelf(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    Identity
		wantErr bool
	}{
		{
			name:   "polystring name and fullName",
			status: http.StatusOK,
			body: `{"user":{"oid":"00000000-0000-0000-0000-000000000002",` +
				`"name":{"orig":"administrator","norm":"administrator"},` +
				`"fullName":{"orig":"midPoint Administrator","norm":"midpoint administrator"},` +
				`"emailAddress":"admin@example.com"}}`,
			want: Identity{
				OID:          "00000000-0000-0000-0000-000000000002",
				Name:         "administrator",
				FullName:     "midPoint Administrator",
				EmailAddress: "admin@example.com",
			},
		},
		{
			name:   "bare string name, no fullName",
			status: http.StatusOK,
			body:   `{"user":{"oid":"abc-123","name":"jdoe","emailAddress":"jdoe@example.com"}}`,
			want: Identity{
				OID:          "abc-123",
				Name:         "jdoe",
				EmailAddress: "jdoe@example.com",
			},
		},
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			body:    `{"error":"invalid credentials"}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    `{"user":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotAuth, gotAccept string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				gotAccept = r.Header.Get("Accept")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			c := NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p3nc1l"})
			got, err := c.Self(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Self() error = nil, want error")
				}
				// A leaked password in an error string would violate the
				// credentials-never-in-logs rule.
				if strings.Contains(err.Error(), "p3nc1l") {
					t.Fatalf("Self() error leaks password: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Self() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Self() = %+v, want %+v", got, tt.want)
			}

			// Request shape: correct endpoint, Basic auth, JSON Accept.
			if gotPath != "/ws/rest/self" {
				t.Errorf("request path = %q, want /ws/rest/self", gotPath)
			}
			wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p3nc1l"))
			if gotAuth != wantAuth {
				t.Errorf("Authorization header = %q, want Basic auth", gotAuth)
			}
			if gotAccept != "application/json" {
				t.Errorf("Accept header = %q, want application/json", gotAccept)
			}
		})
	}
}

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantErr     bool
		wantMissing []string
		wantConfig  Config
	}{
		{
			name: "all present",
			env: map[string]string{
				EnvURL:      "https://mp.example.com/midpoint/",
				EnvUsername: "administrator",
				EnvPassword: "secret",
			},
			wantConfig: Config{
				BaseURL:  "https://mp.example.com/midpoint/",
				Username: "administrator",
				Password: "secret",
			},
		},
		{
			name: "insecure tls opt-in",
			env: map[string]string{
				EnvURL:         "https://localhost:8443/midpoint",
				EnvUsername:    "administrator",
				EnvPassword:    "secret",
				EnvInsecureTLS: "TRUE",
			},
			wantConfig: Config{
				BaseURL:     "https://localhost:8443/midpoint",
				Username:    "administrator",
				Password:    "secret",
				InsecureTLS: true,
			},
		},
		{
			name:        "missing all",
			env:         map[string]string{},
			wantErr:     true,
			wantMissing: []string{EnvURL, EnvUsername, EnvPassword},
		},
		{
			name: "missing password only",
			env: map[string]string{
				EnvURL:      "https://mp.example.com/midpoint",
				EnvUsername: "administrator",
			},
			wantErr:     true,
			wantMissing: []string{EnvPassword},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{EnvURL, EnvUsername, EnvPassword, EnvInsecureTLS} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := ConfigFromEnv()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ConfigFromEnv() error = nil, want error")
				}
				for _, name := range tt.wantMissing {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("error %q does not mention missing var %q", err, name)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ConfigFromEnv() unexpected error: %v", err)
			}
			if got != tt.wantConfig {
				t.Fatalf("ConfigFromEnv() = %+v, want %+v", got, tt.wantConfig)
			}
		})
	}
}
