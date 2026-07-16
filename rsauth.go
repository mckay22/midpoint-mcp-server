package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/mckay22/midpoint-mcp-server/internal/midpoint"
	"github.com/mckay22/midpoint-mcp-server/internal/oidcauth"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// principalClaimKey is the TokenInfo.Extra key carrying the correlated midPoint
// user OID from the verifier to the request middleware.
const principalClaimKey = "midpointOID"

// bearerVerifier verifies an OAuth bearer token and correlates it to a midPoint
// user. correlationAttribute is the midPoint attribute the token's correlation
// claim is matched against ("" = the default, name). Any failure returns an
// ErrInvalidToken-wrapped error, which the SDK surfaces as a 401. The verify +
// correlation run as the service account (no principal in the context), which is
// exactly the identity that holds #proxy.
func bearerVerifier(authn *oidcauth.Authenticator, client *midpoint.Client, correlationAttribute string) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		claims, err := authn.Verify(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
		}
		oid, err := client.CorrelateUser(ctx, claims.Subject, claims.CorrelationValue, correlationAttribute)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
		}
		return &sdkauth.TokenInfo{
			// UserID binds the session to this subject so the transport rejects a
			// different user's token reusing the session (hijack prevention).
			UserID:     claims.Subject,
			Expiration: claims.Expiry,
			Extra:      map[string]any{principalClaimKey: oid},
		}, nil
	}
}

// principalMiddleware copies the correlated midPoint OID from the per-request
// TokenInfo into the context, so the client executes the request as that user
// (Switch-To-Principal). It is a no-op with no token, keeping personal mode
// unchanged.
func principalMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
			if oid, ok := extra.TokenInfo.Extra[principalClaimKey].(string); ok {
				ctx = midpoint.WithPrincipal(ctx, oid)
			}
		}
		return next(ctx, method, req)
	}
}
