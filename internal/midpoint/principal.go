package midpoint

import "context"

// SwitchToPrincipalHeader is midPoint's REST impersonation header: the service
// account executes the request as the user whose OID is given.
const SwitchToPrincipalHeader = "Switch-To-Principal"

type principalKey struct{}

// WithPrincipal returns a context that makes the client execute requests as the
// given midPoint user OID (via the Switch-To-Principal header). This is how
// resource-server mode runs each request as the mapped end user while
// authenticating as the #proxy service account. An empty oid is ignored.
func WithPrincipal(ctx context.Context, oid string) context.Context {
	if oid == "" {
		return ctx
	}
	return context.WithValue(ctx, principalKey{}, oid)
}

// principalFromContext returns the impersonation target OID, or "" if none.
func principalFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(principalKey{}).(string); ok {
		return v
	}
	return ""
}
