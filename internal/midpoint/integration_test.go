//go:build integration

// Integration tests run only under `go test -tags=integration` and exercise the
// read client against a live midPoint (e.g. a 4.10 docker container). They skip
// cleanly when no instance is configured, so a missing container is never a
// failure.
//
// Bring up midPoint 4.10 (Evolveum's official docker image / compose), then:
//
//	MIDPOINT_URL=https://localhost:8443/midpoint \
//	MIDPOINT_USERNAME=administrator \
//	MIDPOINT_PASSWORD=... \
//	MIDPOINT_INSECURE_TLS=true \
//	go test -tags=integration ./internal/midpoint -run Integration -v
package midpoint

import (
	"context"
	"testing"
	"time"
)

func TestIntegrationReadOps(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// self / ping: the authenticated identity anchors the rest of the checks.
	id, err := c.Self(ctx)
	if err != nil {
		t.Fatalf("Self: %v", err)
	}
	if id.OID == "" || id.Name == "" {
		t.Fatalf("Self returned empty identity: %+v", id)
	}
	t.Logf("authenticated as %s (%s)", id.Name, id.OID)

	// search_users should find the authenticated user by name.
	users, err := c.SearchUsers(ctx, SearchOptions{Query: id.Name, Limit: 10})
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) == 0 {
		t.Fatalf("SearchUsers(%q) returned no results", id.Name)
	}

	// get_user by OID round-trips to the same identity.
	u, err := c.GetUser(ctx, id.OID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.OID != id.OID {
		t.Fatalf("GetUser oid = %s, want %s", u.OID, id.OID)
	}

	// get_user_assignments must decode without error.
	asg, err := c.GetUserAssignments(ctx, id.OID)
	if err != nil {
		t.Fatalf("GetUserAssignments: %v", err)
	}
	t.Logf("%s: %d direct assignment(s), %d effective membership(s)",
		id.Name, len(asg.Assignments), len(asg.Effective))

	// A fresh midPoint ships built-in roles; expect at least one.
	roles, err := c.ListRoles(ctx, 50)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) == 0 {
		t.Fatalf("ListRoles returned nothing on a live midPoint")
	}
	t.Logf("listed %d role(s), e.g. %q", len(roles), roles[0].Name)

	// Resources may be empty on a clean instance; assert only that it decodes.
	if _, err := c.ListResources(ctx, 50); err != nil {
		t.Fatalf("ListResources: %v", err)
	}
}
