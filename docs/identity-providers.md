# Resource-server mode: identity-provider setup

This guide is for **resource-server mode** — one shared server, over HTTP, serving
many people who each authenticate with their own OAuth bearer token, so every
request runs as the real human and midPoint's approvals and audit attribute
correctly. (For a single user on their own machine, use **personal mode** — stdio,
your own credentials, no identity provider at all. See the README's
[Two modes](../README.md#two-modes-personal-vs-shared) section.)

Works with **any** OIDC provider — Keycloak, Microsoft Entra ID, Okta, Auth0, Ping,
Google, … There is nothing provider-specific in the server; the differences are
all in how you configure the provider, covered below.

## The one idea that makes the rest simple

**This server is an OAuth _resource server_, not an OAuth _client_.** It never runs
a login flow and never calls the provider on a user's behalf. On each request it
only *validates* the token in the `Authorization: Bearer` header — checking the
signature against the provider's published public keys (JWKS), plus the issuer,
audience, and expiry.

Consequences:

- **The server needs no client secret. Ever.** It only reads the provider's
  *public* discovery document and JWKS.
- The server's entire configuration is two values: **issuer** and **audience**.
- A client secret, if one exists at all, lives on the **client** side (the thing
  that logs the user in — an MCP client such as Claude Desktop or VS Code, or a
  gateway in front of them). Public clients (desktop/CLI, using PKCE) have no
  secret either.

```
  user ──login──▶  Identity Provider  ──issues signed token──▶  MCP client
                        (Keycloak/Entra/Okta/…)                     │
                                                     Authorization: Bearer <token>
                                                                    ▼
   midPoint  ◀── acts as the user (Switch-To-Principal) ──  this server
                                                     (validates token vs JWKS,
                                                      correlates to a midPoint user)
```

## Server configuration (provider-agnostic)

Set two environment variables and start the server on a network address:

```sh
MIDPOINT_MCP_OIDC_ISSUER=https://<your-issuer>       \
MIDPOINT_MCP_OIDC_AUDIENCE=<expected-audience>       \
MIDPOINT_URL=https://midpoint.example.com/midpoint   \
MIDPOINT_USERNAME=svc-mcp MIDPOINT_PASSWORD=…         \
midpoint-mcp-server --http 0.0.0.0:3001
```

- Both OIDC variables must be set together; setting only one is a configuration
  error.
- Binding a non-loopback address is allowed **only** when OIDC is configured.
  Without it the server refuses to start on anything but `127.0.0.1`, so it can
  never expose an unauthenticated network surface.
- `MIDPOINT_USERNAME`/`MIDPOINT_PASSWORD` here are the **service account**, not any
  end user — see [Requirement 3](#requirement-3--the-midpoint-service-account).

The provider is then responsible for three things. Get these right and it works;
they are where every real-world failure comes from.

## Requirement 1 — tokens must carry the expected audience

The server enforces that the token's `aud` claim contains the configured
`MIDPOINT_MCP_OIDC_AUDIENCE`. **This is deliberate and must not be relaxed.**
Audience enforcement is the defense against *token-confusion*: a token your
provider minted for some other service being replayed against this one. A token
whose audience is only the client (`azp`) and not this resource is refused, by
design.

So the provider must issue access tokens whose audience names *this* server.
How you arrange that is provider-specific:

- **Keycloak** — add an **Audience** protocol mapper (type `oidc-audience-mapper`)
  to the client (or a shared client scope) with *Included Custom Audience* set to
  your chosen value (e.g. `midpoint-mcp`) and *Add to access token* on. Keycloak
  omits `aud` by default, so without this the check correctly fails.
- **Microsoft Entra ID** — expose the server as an API and have the client request
  its scope; Entra then sets `aud` to that API automatically (details below). No
  separate "mapper".
- **Okta / Auth0 / others** — register the server as an API / "audience" resource
  and configure the client to request tokens for it.

> Rule of thumb: **decode a real access token and read its `aud` claim**, then set
> `MIDPOINT_MCP_OIDC_AUDIENCE` to exactly that string.

## Requirement 2 — correlation: which midPoint user is this?

Identity comes only from the validated token, never from a tool argument. The
server maps the token to a midPoint user in two steps:

| Step | Token claim | matched against midPoint field |
| --- | --- | --- |
| 1 (always) | `sub` | user `externalId` |
| 2 (fallback, configurable) | `preferred_username` *(default)* | user `name` *(default)* |

The `sub` → `externalId` step always runs first. Step 2 is the fallback and is
**configurable** — see below.

Two good strategies with the defaults:

- **Name match (simplest).** If the provider's `preferred_username` equals the
  midPoint user's `name`, correlation just works. Common when midPoint usernames
  are the corporate email / UPN.
- **`externalId` match (most robust).** Provision each midPoint user's
  `externalId` with a **stable** identifier from the provider, so correlation
  survives a username change.

### Configuring the fallback claim and attribute

If neither default lines up — the provider can't emit a matching
`preferred_username`, or the value you want to correlate on lives in a different
claim (a custom claim, `email`, an employee number, …) — point the fallback at any
claim and any midPoint attribute:

| Variable | Default | Example |
| --- | --- | --- |
| `MIDPOINT_MCP_OIDC_CORRELATION_CLAIM` | `preferred_username` | `email`, `employeeNumber`, `midpoint_username` |
| `MIDPOINT_MCP_OIDC_CORRELATION_ATTRIBUTE` | `name` | `emailAddress`, `employeeNumber` |

For example, to correlate the token's `email` claim against midPoint's
`emailAddress`:

```sh
MIDPOINT_MCP_OIDC_CORRELATION_CLAIM=email
MIDPOINT_MCP_OIDC_CORRELATION_ATTRIBUTE=emailAddress
```

Notes:

- These replace **only** the fallback step; `sub` → `externalId` still runs first.
- The claim value is coerced to a string (a numeric claim such as an employee
  number matches midPoint's string value).
- The attribute is interpolated into a midPoint query filter, so it must be a
  plain path (letters, digits, and `/` — e.g. `emailAddress`,
  `extension/badgeId`); anything else is rejected at startup.
- The value must be **unique** — if two midPoint users match, the request is
  refused rather than guessing.

A token that validates but correlates to **no** midPoint user is refused — that is
the correct outcome for a person who exists in the IdP but not in midPoint.

## Requirement 3 — the midPoint service account

This is **the same for every provider** — it is a midPoint concern, not an IdP one.

The server authenticates to midPoint with the service-account Basic credentials,
then executes each request *as the correlated user* via midPoint's
`Switch-To-Principal` header. For that to be allowed:

- The service account needs the REST authorization action
  `http://midpoint.evolveum.com/xml/ns/public/security/authorization-rest-3#proxy`,
  scoped to the users it may impersonate.
  - **Note:** the superuser role's model-level `#all` authorization does **not**
    include this REST action. Grant it explicitly — e.g. a small role holding just
    that action (scoped by object type `UserType`, or better by archetype) and
    assign it to the service account.
- The **impersonated users** need enough authorization of their own to perform the
  operations, because the request runs as them. A bare user with no authorizations
  cannot even read their own `/self`. In practice assign end users the built-in
  **End user** role (or an equivalent), which also enables the self-service tools
  (`list_requestable_roles`, `request_role`, work items).

## Provider walkthroughs

### Microsoft Entra ID

1. **Register the server as an API.** Entra admin center → *App registrations* →
   *New registration* (e.g. "midPoint MCP"). No redirect URI or client secret is
   needed for the server.
2. **Expose an API.** On that registration → *Expose an API* → set an *Application
   ID URI* (e.g. `api://midpoint-mcp`) → *Add a scope* (e.g. `access`).
3. **Prefer v2.0 access tokens.** In the app *Manifest*, set
   `requestedAccessTokenVersion` (a.k.a. `accessTokenAcceptedVersion`) to `2`.
   v2.0 tokens give a clean issuer (`…/v2.0`) and expose `preferred_username`
   (the UPN); v1.0 tokens use a different issuer host and claim shape.
4. **Let the client request the scope.** The MCP client (or gateway) requests
   `api://midpoint-mcp/access`. Entra then stamps the token's `aud` for that API.
   A confidential client uses a secret; a public/PKCE client does not — either way
   the secret is the client's, not the server's.
5. **Point the server at Entra:**
   ```sh
   MIDPOINT_MCP_OIDC_ISSUER=https://login.microsoftonline.com/<tenant-id>/v2.0
   MIDPOINT_MCP_OIDC_AUDIENCE=api://midpoint-mcp   # or the API app's client-id GUID
   ```

Entra gotchas:

- **Single-tenant issuer.** Use the concrete `…/<tenant-id>/v2.0` issuer. The
  multi-tenant `…/common/…` and `…/organizations/…` authorities publish a
  *templated* issuer and will not match a token's `iss` as-is.
- **`aud` may be the GUID, not the URI.** Depending on configuration, v2.0 tokens
  carry `aud` = the API app's **client-id** rather than the `api://…` URI. Decode a
  real token and set `MIDPOINT_MCP_OIDC_AUDIENCE` to whatever it actually says.
- **Correlation.** `preferred_username` is the UPN (e.g. `bob@contoso.com`); match
  it to midPoint `name`, or provision midPoint `externalId` from a stable Entra id.
  (Entra's `sub` is *pairwise* — unique per application — so if you correlate on
  `sub` → `externalId` you must store the value this app sees. Entra's `oid` claim
  is the immutable, cross-application object id; to correlate on it set
  `MIDPOINT_MCP_OIDC_CORRELATION_CLAIM=oid` against whatever midPoint attribute
  holds it — see [Requirement 2](#requirement-2--correlation-which-midpoint-user-is-this).)

### Keycloak

1. **Client for the users.** A realm client with *Direct access grants* or the
   standard auth-code + PKCE flow, per how your MCP client logs in.
2. **Audience mapper.** Add an `oidc-audience-mapper` (see
   [Requirement 1](#requirement-1--tokens-must-carry-the-expected-audience)) with a
   custom audience such as `midpoint-mcp`, added to the access token.
3. **Point the server at the realm:**
   ```sh
   MIDPOINT_MCP_OIDC_ISSUER=https://keycloak.example.com/realms/<realm>
   MIDPOINT_MCP_OIDC_AUDIENCE=midpoint-mcp
   ```
   The issuer must equal the token's `iss` exactly — including host and port. If
   Keycloak runs behind a different front-end URL than you reach it on, tokens'
   `iss` uses the *configured front-end URL*; set the issuer to that and make sure
   the server can resolve it.

### Generic OIDC (Okta, Auth0, Ping, …)

The pattern is always the same:

1. Register/define the server as an **API / audience resource**.
2. Configure the client to request tokens **for that audience**.
3. Set `MIDPOINT_MCP_OIDC_ISSUER` to the issuer from the provider's
   `/.well-known/openid-configuration` (its `issuer` field), and
   `MIDPOINT_MCP_OIDC_AUDIENCE` to the audience you configured.
4. Make sure `preferred_username` or `sub` correlates to a midPoint user.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Server won't bind a non-loopback address | OIDC not configured — set issuer **and** audience. |
| Startup/discovery error | Issuer URL wrong or unreachable; the server can't fetch `<issuer>/.well-known/openid-configuration`. |
| Every request `401`, "issuer did not match" | `MIDPOINT_MCP_OIDC_ISSUER` ≠ the token's `iss` (host/port/version mismatch — classic behind proxies or Entra v1 vs v2). |
| Every request `401`, audience/`aud` | Token's `aud` doesn't contain `MIDPOINT_MCP_OIDC_AUDIENCE`; provider isn't issuing the audience (Keycloak mapper missing, or client didn't request the API scope). Do **not** fix this by relaxing the check. |
| Request refused, "no midPoint user matches …" | Correlation: neither `sub`→`externalId` nor `preferred_username`→`name` hit a user. |
| Operation fails with midPoint "Access denied" (HTTP 500) | Service account lacks `…rest-3#proxy`, **or** the impersonated user lacks authorization for the operation (give them the End user role). |

## See also

- [README — Two modes](../README.md#two-modes-personal-vs-shared)
- [README — HTTP transport / resource-server mode](../README.md#http-transport)
