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
	"os"
	"strings"
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

// TestIntegrationWriteRoundTrip exercises the M2 disable→enable round-trip
// against a live midPoint. It runs only when the write gate is on, so a
// read-only integration run is never blocked by it.
func TestIntegrationWriteRoundTrip(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	if !cfg.AllowWrites {
		t.Skipf("skipping write round-trip: set %s=true to run", EnvAllowWrites)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const name = "mcp-it-user"
	oid := findOrCreateUser(ctx, t, c, name)

	// disable → status becomes disabled
	applyPlan(ctx, t, c, func() (Plan, error) { return c.PlanSetUserEnabled(oid, false) })
	if got := userStatus(ctx, t, c, oid); got != "disabled" {
		t.Fatalf("after disable, status = %q, want disabled", got)
	}

	// enable → status becomes enabled again
	applyPlan(ctx, t, c, func() (Plan, error) { return c.PlanSetUserEnabled(oid, true) })
	if got := userStatus(ctx, t, c, oid); got != "enabled" {
		t.Fatalf("after enable, status = %q, want enabled", got)
	}
	t.Logf("disable→enable round-trip OK for %s (%s)", name, oid)
}

// TestIntegrationApprovalRoundTrip drives the M3 acceptance flow against a live
// midPoint: request a role → an approval case opens → approve the work item →
// the assignment appears on the user. It needs a role guarded by an approval
// policy (and the caller able to approve it), so it runs only when both the
// write gate and MIDPOINT_IT_APPROVAL_ROLE_OID are set.
func TestIntegrationApprovalRoundTrip(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	if !cfg.AllowWrites {
		t.Skipf("skipping approval round-trip: set %s=true to run", EnvAllowWrites)
	}
	roleOID := strings.TrimSpace(os.Getenv("MIDPOINT_IT_APPROVAL_ROLE_OID"))
	if roleOID == "" {
		t.Skip("skipping approval round-trip: set MIDPOINT_IT_APPROVAL_ROLE_OID to an approval-guarded role")
	}

	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	userOID := findOrCreateUser(ctx, t, c, "mcp-it-approval-user")

	// Request the role for the test user (the requester is our authenticated self).
	reqPlan, err := c.PlanRequestRole(userOID, roleOID)
	if err != nil {
		t.Fatalf("PlanRequestRole: %v", err)
	}
	if _, err := c.Apply(ctx, reqPlan); err != nil {
		t.Fatalf("requesting role: %v", err)
	}

	// The case is created asynchronously; poll briefly.
	var caseOID string
	for i := 0; i < 15 && caseOID == ""; i++ {
		caseOID = c.FindRequestCase(ctx, userOID, roleOID)
		if caseOID == "" {
			time.Sleep(time.Second)
		}
	}
	if caseOID == "" {
		t.Skipf("no approval case opened for role %s — is it guarded by an approval policy?", roleOID)
	}
	t.Logf("approval case opened: %s", caseOID)

	detail, err := c.GetCase(ctx, caseOID)
	if err != nil {
		t.Fatalf("GetCase: %v", err)
	}
	if len(detail.WorkItems) == 0 {
		t.Fatalf("case %s has no work items", caseOID)
	}

	// Approve the first work item.
	appPlan, err := c.PlanCompleteWorkItem(caseOID, detail.WorkItems[0].ID, true, "approved by integration test")
	if err != nil {
		t.Fatalf("PlanCompleteWorkItem: %v", err)
	}
	if _, err := c.Apply(ctx, appPlan); err != nil {
		t.Fatalf("approving work item: %v", err)
	}

	// After approval the assignment should materialize; poll briefly.
	assigned := false
	for i := 0; i < 15 && !assigned; i++ {
		asg, err := c.GetUserAssignments(ctx, userOID)
		if err != nil {
			t.Fatalf("GetUserAssignments: %v", err)
		}
		for _, m := range asg.Effective {
			if m.OID == roleOID {
				assigned = true
				break
			}
		}
		if !assigned {
			time.Sleep(time.Second)
		}
	}
	if !assigned {
		t.Fatalf("role %s did not appear on user %s after approval", roleOID, userOID)
	}
	t.Logf("approval round-trip OK: role %s now assigned to %s", roleOID, userOID)
}

// TestIntegrationListRequestableRoles exercises the self-service catalog query
// live. A clean instance may have zero requestable roles, so it asserts the call
// succeeds and any returned roles decode with populated fields — not a count.
func TestIntegrationListRequestableRoles(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	roles, err := c.ListRequestableRoles(ctx, 50)
	if err != nil {
		t.Fatalf("ListRequestableRoles: %v", err)
	}
	for i, r := range roles {
		if r.OID == "" || r.Name == "" {
			t.Errorf("requestable role %d has empty oid/name: %+v", i, r)
		}
	}
	t.Logf("list_requestable_roles returned %d role(s)", len(roles))
}

// TestIntegrationTeam exercises the manager/team queries live. A clean instance
// may have no org structure, so it asserts the calls succeed and any results
// decode with populated fields — not a count. It also proves the relation-scoped
// parentOrgRef query shape is accepted by midPoint.
func TestIntegrationTeam(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	team, err := c.ListMyTeam(ctx, 50)
	if err != nil {
		t.Fatalf("ListMyTeam: %v", err)
	}
	managers, err := c.ListMyManagers(ctx, 50)
	if err != nil {
		t.Fatalf("ListMyManagers: %v", err)
	}
	for _, u := range append(append([]UserSummary{}, team...), managers...) {
		if u.OID == "" || u.Name == "" {
			t.Errorf("team/manager result has empty oid/name: %+v", u)
		}
	}
	t.Logf("list_my_team=%d, list_my_managers=%d", len(team), len(managers))
}

// TestIntegrationSearchObjects exercises the generic object search live.
func TestIntegrationSearchObjects(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	users, err := c.SearchObjects(ctx, "users", "", 10)
	if err != nil {
		t.Fatalf("SearchObjects(users): %v", err)
	}
	if len(users) == 0 {
		t.Fatal("SearchObjects(users) returned nothing on a live midPoint")
	}
	if _, err := c.SearchObjects(ctx, "roles", "", 10); err != nil {
		t.Fatalf("SearchObjects(roles): %v", err)
	}
	t.Logf("search_objects OK: %d user(s), e.g. %q", len(users), users[0].Name)
}

// TestIntegrationSearchAudit exercises the audit search live. It runs an
// execute-script action, so it needs script-execution authorization (and does
// not work under OIDC #proxy impersonation); when the environment can't grant
// that, it skips rather than fails. When records come back, their fields must be
// populated — that guards the executeScript-response parse path.
func TestIntegrationSearchAudit(t *testing.T) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Skipf("skipping live integration test: %v", err)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := c.SearchAudit(ctx, AuditQuery{Limit: 5})
	if err != nil {
		t.Skipf("search_audit unavailable — likely no script-execution authorization: %v", err)
	}
	t.Logf("search_audit returned %d record(s), status=%q", len(res.Records), res.Status)
	for i, r := range res.Records {
		if r.Timestamp == "" || r.EventType == "" {
			t.Errorf("record %d has empty timestamp/eventType (parse regression?): %+v", i, r)
		}
	}
	// A live instance always has audit activity (logins, etc.); a filter-free
	// query returning nothing points at a broken search, not an empty trail.
	if res.Status == "success" && len(res.Records) == 0 {
		t.Errorf("no audit records parsed from a live instance with status=success")
	}
}

func findOrCreateUser(ctx context.Context, t *testing.T, c *Client, name string) string {
	t.Helper()
	users, err := c.SearchUsers(ctx, SearchOptions{Query: name, Limit: 5})
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	for _, u := range users {
		if u.Name == name {
			return u.OID
		}
	}
	plan, err := c.PlanCreateUser(UserSpec{Name: name, FullName: "MCP Integration Test User"})
	if err != nil {
		t.Fatalf("PlanCreateUser: %v", err)
	}
	res, err := c.Apply(ctx, plan)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	if res.OID == "" {
		t.Fatalf("create %s returned no oid (status %d)", name, res.StatusCode)
	}
	return res.OID
}

func applyPlan(ctx context.Context, t *testing.T, c *Client, build func() (Plan, error)) {
	t.Helper()
	plan, err := build()
	if err != nil {
		t.Fatalf("building plan: %v", err)
	}
	if _, err := c.Apply(ctx, plan); err != nil {
		t.Fatalf("applying %q: %v", plan.Summary, err)
	}
}

func userStatus(ctx context.Context, t *testing.T, c *Client, oid string) string {
	t.Helper()
	u, err := c.GetUser(ctx, oid)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	return u.Status
}
