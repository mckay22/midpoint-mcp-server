package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
)

// CorrelateUser maps an OIDC identity to a midPoint user OID. It first tries the
// token subject against the user's externalId, then falls back to the
// preferred_username against the user's name. The externalId attempt is
// best-effort: if a deployment's schema has no externalId path the search may
// error, in which case correlation falls through to the name match.
//
// This is how resource-server mode resolves "who is the human behind this
// token"; the resulting OID is the Switch-To-Principal target.
func (c *Client) CorrelateUser(ctx context.Context, subject, preferredUsername string) (string, error) {
	if subject != "" {
		if oid, err := c.uniqueUserByFilter(ctx, fmt.Sprintf("externalId = %s", quoteQueryString(subject))); err == nil && oid != "" {
			return oid, nil
		}
	}
	if preferredUsername != "" {
		oid, err := c.uniqueUserByFilter(ctx, fmt.Sprintf("name = %s", quoteQueryString(preferredUsername)))
		if err != nil {
			return "", err
		}
		if oid != "" {
			return oid, nil
		}
	}
	return "", fmt.Errorf("no midPoint user matches subject=%q preferred_username=%q", subject, preferredUsername)
}

// uniqueUserByFilter returns the OID of the single user matching filter, "" if
// none, or an error if the match is ambiguous.
func (c *Client) uniqueUserByFilter(ctx context.Context, filter string) (string, error) {
	raws, err := c.searchRaw(ctx, collUsers, filter, 2)
	if err != nil {
		return "", err
	}
	if len(raws) == 0 {
		return "", nil
	}
	if len(raws) > 1 {
		return "", fmt.Errorf("ambiguous correlation: %d users match %q", len(raws), filter)
	}
	var u userJSON
	if err := json.Unmarshal(raws[0], &u); err != nil {
		return "", fmt.Errorf("decoding correlated user: %w", err)
	}
	return u.OID, nil
}
