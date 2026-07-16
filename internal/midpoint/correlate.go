package midpoint

import (
	"context"
	"encoding/json"
	"fmt"
)

// DefaultCorrelationAttribute is the midPoint attribute the correlation value is
// matched against when a deployment does not configure one.
const DefaultCorrelationAttribute = "name"

// CorrelateUser maps an OIDC identity to a midPoint user OID. It first tries the
// token subject against the user's externalId, then falls back to matching
// correlationValue against correlationAttribute (defaulting to the user's name).
// The externalId attempt is best-effort: if a deployment's schema has no
// externalId path the search may error, in which case correlation falls through
// to the attribute match.
//
// correlationValue is whichever token claim the deployment correlates on
// (preferred_username by default); correlationAttribute is the midPoint attribute
// that holds it (name by default). The attribute is interpolated into the query
// filter, so callers must pass a validated path — see ValidCorrelationAttribute.
//
// This is how resource-server mode resolves "who is the human behind this
// token"; the resulting OID is the Switch-To-Principal target.
func (c *Client) CorrelateUser(ctx context.Context, subject, correlationValue, correlationAttribute string) (string, error) {
	attr := correlationAttribute
	if attr == "" {
		attr = DefaultCorrelationAttribute
	}
	if subject != "" {
		if oid, err := c.uniqueUserByFilter(ctx, fmt.Sprintf("externalId = %s", quoteQueryString(subject))); err == nil && oid != "" {
			return oid, nil
		}
	}
	if correlationValue != "" {
		oid, err := c.uniqueUserByFilter(ctx, fmt.Sprintf("%s = %s", attr, quoteQueryString(correlationValue)))
		if err != nil {
			return "", err
		}
		if oid != "" {
			return oid, nil
		}
	}
	return "", fmt.Errorf("no midPoint user matches subject=%q %s=%q", subject, attr, correlationValue)
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
