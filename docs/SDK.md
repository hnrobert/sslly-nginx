# Client SDKs

The control API contract lives in
[buf.build/sslly-nginx/sslly-nginx](https://buf.build/sslly-nginx/sslly-nginx) and
the Buf Schema Registry builds and hosts generated SDKs for it — nothing is
published to npmjs.com / PyPI / nuget.org, and consumer projects need no
protoc toolchain. The server side speaks:

- **HTTP/JSON gateway** — `POST http://<host>:9080/v1/<RpcName>` with an
   `Authorization: Bearer <token>` header (see [API.md](API.md)).
- **Native gRPC** — `<host>:9081`, same bearer token as `authorization`
   metadata. There is **no** Connect-protocol or gRPC-Web endpoint.

## Go

Hosted Go modules via the standard module proxy:

```bash
go get buf.build/gen/go/sslly-nginx/sslly-nginx/protocolbuffers/go@latest
go get buf.build/gen/go/sslly-nginx/sslly-nginx/grpc/go@latest
```

```go
import (
   ssllyv1 "buf.build/gen/go/sslly-nginx/sslly-nginx/protocolbuffers/go/hnrobert/sslly/v1"
   ssllyv1grpc "buf.build/gen/go/sslly-nginx/sslly-nginx/grpc/go/hnrobert/sslly/v1"
   "google.golang.org/grpc"
   "google.golang.org/grpc/credentials/insecure"
   "google.golang.org/grpc/metadata"
)

conn, _ := grpc.NewClient("host:9081",
   grpc.WithTransportCredentials(insecure.NewCredentials()))
client := ssllyv1grpc.NewProxyServiceClient(conn)

ctx := metadata.AppendToOutgoingContext(context.Background(),
   "authorization", "Bearer "+token)
resp, err := client.ListProxyEntries(ctx, &ssllyv1.ListProxyEntriesRequest{})
```

Notes: pin a release with `@v1.2.3` (the git tag). Fresh pushes can take
~30 min to appear through the public proxy; `GOPRIVATE=buf.build/gen/go`
bypasses the wait.

## TypeScript / JavaScript

Point npm at the BSR registry (project `.npmrc`):

```text
@buf:registry=https://buf.build/gen/npm/v1
```

```bash
npm install @buf/sslly-nginx_sslly-nginx.bufbuild_es @bufbuild/protobuf
```

**Recommended: typed fetch against the gateway.** The generated
`*_pb.ts` files give you `toJson`/`fromJson` for every message, which fits
the JSON gateway directly:

```ts
import { ListProxyEntriesRequest, ListProxyEntriesResponse } from "@buf/sslly-nginx_sslly-nginx.buf.build_es/hnrobert/sslly/v1/proxy_pb.js";

async function listProxyEntries(base: string, token: string) {
   const res = await fetch(`${base}/v1/ListProxyEntries`, {
      method: "POST",
      headers: {
         Authorization: `Bearer ${token}`,
         "Content-Type": "application/json",
      },
      body: ListProxyEntriesRequest.toJsonString(new ListProxyEntriesRequest()),
   });
   if (!res.ok) throw new Error(`list failed: ${res.status}`);
   return ListProxyEntriesResponse.fromJson(await res.json());
}
```

**Node alternative — native gRPC:** install
`@buf/sslly-nginx_sslly-nginx.connectrpc_es` plus `@connectrpc/connect` and
`@connectrpc/connect-node`, and dial `:9081` with the **gRPC transport**
(`createNodeGrpcTransport`); the Connect and gRPC-Web transports do not
match this server.

## Python

Hosted PEP 503 index (not PyPI):

```bash
pip install --extra-index-url https://buf.build/gen/python \
   sslly-nginx-sslly-nginx-protocolbuffers-python \
   sslly-nginx-sslly-nginx-grpc-python
```

```python
import grpc
from hnrobert.sslly.v1 import proxy_pb2, proxy_pb2_grpc

channel = grpc.insecure_channel("host:9081")
stub = proxy_pb2_grpc.ProxyServiceStub(channel)
resp = stub.ListProxyEntries(
    proxy_pb2.ListProxyEntriesRequest(),
    metadata=(("authorization", f"Bearer {token}"),),
)
```

## C# / .NET

Add the BSR NuGet feed to `NuGet.config` (username is arbitrary, the
password is a [BSR token](https://buf.build/settings)):

```xml
<configuration>
  <packageSources>
    <clear />
    <add key="nuget.org" value="https://api.nuget.org/v3/index.json" />
    <add key="BSR" value="https://buf.build/gen/nuget/index.json" />
  </packageSources>
  <packageSourceMapping>
    <packageSource key="nuget.org">
      <package pattern="*" />
    </packageSource>
    <packageSource key="BSR">
      <package pattern="BSR.*" />
    </packageSource>
  </packageSourceMapping>
  <packageSourceCredentials>
    <BSR>
      <add key="Username" value="token" />
      <add key="ClearTextPassword" value="%BUF_TOKEN%" />
      <add key="ValidAuthenticationTypes" value="Basic" />
    </BSR>
  </packageSourceCredentials>
</configuration>
```

```bash
dotnet add package BSR.SsllyNginx.SsllyNginx.Protocolbuffers.Csharp
dotnet add package BSR.SsllyNginx.SsllyNginx.Grpc.Csharp
```

Generated namespace: `Hnrobert.Sslly.V1`. Tokenless fallback: copy
[`proto/buf.gen.csharp.yaml`](../proto/buf.gen.csharp.yaml) from this repo
and run `buf generate buf.build/sslly-nginx/sslly-nginx --include-imports` —
the remote plugins run on BSR, no local toolchain needed.

## Generating locally (any language, custom options)

The repo ships reference templates — `proto/buf.gen.{go,ts,python,csharp}.yaml` —
for consumers who need plugin options the hosted SDKs don't expose:

```bash
mkdir my-sdk && cd my-sdk
# buf.yaml with: deps: [buf.build/sslly-nginx/sslly-nginx]
buf dep update
buf generate buf.build/sslly-nginx/sslly-nginx --template buf.gen.<lang>.yaml --include-imports
```

## Versioning

- Every push of `proto/**` to `main` publishes a new module commit on the
  default label — consumers on `@latest`/`@main` track it.
- Every `v*` git tag publishes the same content under a **label** matching
  the tag: pin with `go get ...@v1.2.3` /
  `npm install @buf/sslly-nginx_sslly-nginx.buf.build_es@v1.2.3`. The label
  resolves to the concrete SDK version (`PLUGIN_VERSION-YYYYMMDDHHMMSS-COMMIT12.REVISION`).
- Pre-release labels (cut from `develop`, e.g. `v1.2.4-rc1`) sort below all
  released versions automatically.
- Labels are mutable pointers, commits are immutable. **Never move a
  released label** — cut a new tag instead. Labels can be archived on the
  BSR but not deleted.
- Resolve exact versions with
  `buf registry sdk version --module=buf.build/sslly-nginx/sslly-nginx --plugin=buf.build/protocolbuffers/go`.

Background: [Generated SDKs](https://buf.build/docs/bsr/generated-sdks/),
[commits & labels](https://buf.build/docs/bsr/commits-labels/).
