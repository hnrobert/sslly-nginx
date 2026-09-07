# Control API Reference

sslly-nginx ships a web control API for reading and mutating its YAML
configuration over HTTP. The contract is defined in protobuf
(`proto/hnrobert/sslly/v1/`), served as **both** a native gRPC service and an
HTTP/JSON facade via grpc-gateway — every endpoint is a `POST` with a JSON
body.

## Endpoints & environment

| Variable                 | Default | Purpose                          |
| ------------------------ | ------- | -------------------------------- |
| `SSLLY_API_HTTP_ADDR`    | `:9080` | HTTP/JSON gateway (this API)     |
| `SSLLY_API_GRPC_ADDR`    | `:9081` | Native gRPC (grpcurl / clients)  |
| `SSLLY_API_ADMIN_TOKEN`  | —       | Bootstrap admin token (see Auth) |

The compose file binds both ports on the host network. `GET /healthz` on the
HTTP port answers unauthenticated `{"status":"ok"}` for probes.

### Client SDKs

The protobuf contract is published to the
[Buf Schema Registry](https://buf.build/sslly-nginx/sslly-nginx), which builds
and hosts generated SDKs for Go, TypeScript, Python, and .NET — other
projects install a package and call the API, no protoc needed. Quickstarts
for every language: [SDK.md](SDK.md).

## Authentication

Every call needs a bearer token:

```bash
curl -s -X POST localhost:9080/v1/ListProxyEntries \
   -H "Authorization: Bearer <token>"
```

Tokens are SHA-256-hashed in `configs/users.yaml` (never stored in
plaintext). On first boot sslly-nginx creates an `admin` user with full
access: the token comes from `SSLLY_API_ADMIN_TOKEN` if set, otherwise a
random 24-byte token is generated and printed to the log **exactly once**.
Generate hashes for new tokens with:

```bash
printf '%s' 'your-secret-token' | shasum -a 256    # macOS
printf '%s' 'your-secret-token' | sha256sum        # Linux
```

As a convenience, a user entry may carry `token: <plaintext>` instead —
usable immediately on a running service (hot reloads verify against it
directly and never rewrite the file). On the **next cold start** the app
converts it to `token_hash`, strips the plaintext, and marks the line with a
`# converted from token at startup` comment. While a `token` field
exists it is the authoritative credential — a stale `token_hash` beside it
is ignored.

A missing or malformed `users.yaml` fails **closed**: every authenticated
call is rejected until the file is fixed. Permission changes apply on the
next request (the file is cached by mtime) and never trigger an nginx
reload.

## Permissions (RBAC)

Each user holds permission rules in `configs/users.yaml`:

```yaml
users:
   - name: admin
      token_hash: <sha256 hex>
      permissions:
         - surface: proxy        # proxy | cors | logs | users
            mode: read-write     # read | read-write
            # domains: ["*.ibuduan.com"]  # exact or *.suffix (subdomains only)
            # upstreams: ["8080", "192.168.50.2:1234"]  # verbatim upstream keys
```

Semantics — deny by default, and **one rule must cover the whole request**
(rules never combine):

1. **Surface**: `proxy` (proxy.yaml entries + `no_trailing_slash`), `cors`
   (cors.yaml rules), `logs` (logs.yaml), `users` (user management).
2. **Mode**: `read` satisfies read RPCs; `read-write` is required for
   mutations. List/get RPCs **filter out** entries the caller cannot see.
3. **Selectors** (both dimensions must cover every resource of a request):
   - `domains`: exact domain or `*.suffix` matching subdomains **only**
     (same semantics as cors.yaml keys — `*.example.com` does not match the
     bare `example.com`). `*` or an absent list = unrestricted.
   - `upstreams`: exact proxy.yaml upstream keys, matched **verbatim**.
     `*` or absent = unrestricted.
   - A resource without a domain (e.g. a stream target `8122`) never matches
     a domain-restricted rule; the cors catch-all key `*` is only covered by
     an unrestricted (or `*`) domain selector.
4. Writes authorize the **complete new state**, not the delta — a scoped
   user cannot widen an entry beyond their grant.
5. **Last-admin guard**: user changes that would leave zero `users`-scope
   read-write admins are rejected (`FailedPrecondition`).

## Endpoint reference

All routes are `POST <prefix>/<RpcName>` on the HTTP port. The **prefix is a
serving-layer decision** (`APIPrefix` in `internal/api/server.go`, currently
`/v1`) — the proto annotations carry only the bare RPC method name, so
bumping the URL version or changing the namespace never touches the protos.
JSON fields are camelCase. `apply` in responses reports the reload outcome:
`{"applied": true}` or `{"applied": false, "error": "health check: ..."}`
with the YAML rolled back.

| RPC                | Path                     | Body                                        |
| ------------------ | ------------------------ | ------------------------------------------- |
| ListProxyEntries   | `/v1/ListProxyEntries`   | `{}`                                        |
| SetProxyEntry      | `/v1/SetProxyEntry`      | `{"entry": {...}}`                          |
| DeleteProxyEntry   | `/v1/DeleteProxyEntry`   | `{"upstreamKey": "1234"}`                   |
| GetNoTrailingSlash | `/v1/GetNoTrailingSlash` | `{}`                                        |
| SetNoTrailingSlash | `/v1/SetNoTrailingSlash` | `{"listenerKeys": ["a/b"]}`                 |
| ListCorsRules      | `/v1/ListCorsRules`      | `{}`                                        |
| SetCorsRule        | `/v1/SetCorsRule`        | `{"rule": {...}, "updateMask": "..."}`      |
| DeleteCorsRule     | `/v1/DeleteCorsRule`     | `{"key": "*.example.com"}`                  |
| GetLogsConfig      | `/v1/GetLogsConfig`      | `{}`                                        |
| UpdateLogsConfig   | `/v1/UpdateLogsConfig`   | `{"config": {...}, "updateMask": "..."}`    |
| ListUsers          | `/v1/ListUsers`          | `{}`                                        |
| UpsertUser         | `/v1/UpsertUser`         | `{"user": {...}, "token": "new-plaintext"}` |
| DeleteUser         | `/v1/DeleteUser`         | `{"name": "ops"}`                           |

The same names work over native gRPC (`hnrobert.sslly.v1.ProxyService.ListProxyEntries` etc.).

### ProxyService — `proxy.yaml`

```json
{
   "entry": {
      "upstreamKey": "9099",
      "listenerKeys": ["api.example.com", "api.example.com|8443"]
   }
}
```

### CorsService — `cors.yaml`

`updateMask` is a comma-separated
[FieldMask](https://protobuf.dev/reference/go/api-docs/google.protobuf.fieldmask)
in camelCase (`"allowOrigin"`, `"allowHeaders"`, ...). An **empty mask
writes every field**; only masked fields are touched otherwise — matching
cors.yaml presence semantics:

- a masked field set to `""` or `[]` **clears** it (renders `""` / `[]` in
  the file),
- an unmasked field keeps its on-disk state (inherits at generation time).

List responses include `explicitFields` to distinguish cleared from unset.

### LogsService — `logs.yaml`

Mask paths: `ssllyLevel`, `nginxLevel`, `nginx.stderrAs`, `nginx.stderrShow`
(camelCase; empty mask = all). Allowed values: levels
`debug|info|warn|error`, stderr fields `warn|error`.

### UsersService — `users.yaml`

Enum values use their full names, e.g.
`"surface": "E_PERMISSION_SURFACE_CORS"`, `"mode": "E_PERMISSION_MODE_READ_WRITE"`.
An empty `token` keeps the existing hash; responses never contain token
material. Changes apply immediately (no reload).

## How mutations are applied

1. Authorize → validate the request (`InvalidArgument` on bad input).
2. Edit the YAML file with **comment- and order-preserving** surgery, written
   atomically (temp file + rename). New keys are appended at the end.
3. Re-load and validate the whole config (`ValidateConfig`); fatal problems
   restore the previous file bytes and return `applied: false` — nginx is
   never bothered.
4. Run the validated reload pipeline synchronously: snapshot → regenerate →
   `nginx -t` → SIGHUP → health check → commit, with automatic rollback of
   nginx **and** the YAML on failure.
5. The fsnotify watcher's debounced duplicate reload is skipped via a config
   hash comparison (SSL changes always force a reload).

## Walkthrough

```bash
# Bootstrap admin token (first boot, via env):
SSLLY_API_ADMIN_TOKEN=dev-admin-token ./bin/sslly-nginx

TOKEN="Authorization: Bearer dev-admin-token"

# Add a route; response reports the reload outcome:
curl -s -X POST localhost:9080/v1/SetProxyEntry \
   -H "$TOKEN" -H "Content-Type: application/json" \
   -d '{"entry":{"upstreamKey":"9099","listenerKeys":["api.local.test"]}}'

# Scoped operator: CORS control under *.ibuduan.com only:
curl -s -X POST localhost:9080/v1/UpsertUser \
   -H "$TOKEN" -H "Content-Type: application/json" \
   -d '{"user":{"name":"ops","permissions":[{"surface":"E_PERMISSION_SURFACE_CORS","mode":"E_PERMISSION_MODE_READ_WRITE","domains":["*.ibuduan.com"]}]},"token":"ops-token"}'

curl -s -X POST localhost:9080/v1/SetCorsRule \
   -H "Authorization: Bearer ops-token" -H "Content-Type: application/json" \
   -d '{"rule":{"key":"api.ibuduan.com","allowOrigin":"https://app.ibuduan.com"},"updateMask":"allowOrigin"}'

# Native gRPC:
grpcurl -plaintext -H "authorization: Bearer dev-admin-token" -d '{}' \
   localhost:9081 hnrobert.sslly.v1.ProxyService.ListProxyEntries
```

Error responses carry gRPC codes as JSON (`401` unauthenticated, `403`
permission denied, `400` invalid argument, `404` not found).
