# sccache compatibility research — 2026-07-22

> Research record, frozen on 2026-07-22. Only first-party project
> documentation, release notes and source code are used. The stable target is
> sccache v0.15.0; current `main` was also checked for changes that affect the
> proposed integration.

> **Implementation status:** this document is a frozen pre-implementation
> baseline. The compatibility gaps below were closed in Depsilo on 2026-07-22;
> see [the compiler-cache guide](../compile-cache.md) for the supported setup.
> “Current” and “blocked” wording below describes the state at research time.

## Executive conclusion

- **sccache is a strong target for Depsilo.** It is an actively maintained,
  cross-platform compiler wrapper for C/C++, Rust, CUDA, HIP and other
  compilers, with local, cloud and multi-level caches. The latest stable
  release at the research date is
  [v0.15.0](https://github.com/mozilla/sccache/releases/tag/v0.15.0), whose
  release commit is
  [`c2e4b4ca035f91f8b63978afc8e2c71f174fe306`](https://github.com/mozilla/sccache/commit/c2e4b4ca035f91f8b63978afc8e2c71f174fe306).
  The inspected `main` snapshot is
  [`7cd4f2c396a4712fe7627d5209083b7127841296`](https://github.com/mozilla/sccache/commit/7cd4f2c396a4712fe7627d5209083b7127841296),
  committed on 2026-07-21.
- **Depsilo was not directly compatible at the start of this research.**
  The appropriate client integration is sccache's WebDAV backend. Bearer
  authentication and length-delimited `PUT` already align with Depsilo, but
  four wire-contract differences block stock sccache: 64-character keys,
  `a/b/c/<full-key>` paths, the `.sccache_check` object, and WebDAV
  `PROPFIND`/`MKCOL` calls before writes.
- **Do not present this as ccache and sccache sharing entries.** Depsilo may
  serve both clients, but sccache computes different keys and stores its own
  ZIP-with-zstd entry format. They should use protocol-specific endpoints and
  object namespaces.
- **Do not reuse `sccache-dist` as the cache server.** It schedules and executes
  remote compilation jobs. It is not a generic remote artifact-cache service.
- Recommended implementation: add a narrow
  `/sccache/v1/{namespace}` compatibility adapter over the existing compile
  cache storage, quota, LRU and credential subsystems. Avoid exposing a
  general-purpose WebDAV filesystem.

## Version and project assessment

sccache describes itself as a ccache-like compiler wrapper that stores results
on local disk or external storage, and its supported compiler set includes
Assembler, C/C++, Rust, CUDA and HIP
([README](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/README.md#L10-L17)).
The v0.15.0 release added multi-tier caching with fallback and automatic
backfill and continued expanding compiler/platform coverage
([release notes](https://github.com/mozilla/sccache/releases/tag/v0.15.0#summary)).

This makes it materially broader than ccache for teams that build both native
and Rust code. It is also a good fit for Depsilo's existing machine-credential,
namespace, quota and observability model. The main engineering caution is that
"remote cache" is a family of storage adapters, not one stable, sccache-owned
network protocol.

## Architecture and the server boundary

The normal execution path is:

```text
compiler invocation
  -> sccache wrapper/client
  -> local sccache daemon (normally 127.0.0.1:4226)
  -> selected storage backend(s)
```

The official README explicitly says the client/server daemon runs on the same
machine and defaults to `127.0.0.1:4226`
([source](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/README.md#L169-L180)).
That daemon is an optimization boundary for compiler requests; it is not the
shared HTTP cache service that Depsilo would replace.

Distributed compilation is a separate system with three roles: client,
`sccache-dist` scheduler and `sccache-dist` build server
([architecture](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/Distributed.md#L18-L32)).
Its HTTP API allocates jobs, submits toolchains and executes builds, with
`bincode` request/response bodies
([API list](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/Distributed.md#L34-L60)).
Therefore:

- `sccache --start-server` is a local compiler-cache daemon;
- `sccache-dist scheduler/server` is a remote execution cluster;
- neither is a generic, multi-tenant remote cache server comparable to
  Depsilo's compile-cache data plane.

sccache expects operators to supply a storage service such as S3, Redis or
WebDAV. Depsilo can occupy that storage-service role through the WebDAV adapter.

## Remote storage backends and configuration

The stable README lists local disk, S3/R2, Redis, Memcached, Google Cloud
Storage, Azure Blob, GitHub Actions cache, WebDAV, Alibaba OSS and Tencent COS
([backend list](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/README.md#L35-L47)).

| Backend | Connection/configuration model | Relevance to Depsilo |
|---|---|---|
| WebDAV over HTTP(S) | `SCCACHE_WEBDAV_ENDPOINT`, optional key prefix, Basic username/password or bearer token. The official document labels ccache HTTP, Bazel and Gradle cache services as usable backends ([WebDAV docs](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/Webdav.md#L1-L18)). | **Recommended integration seam**, after implementing the exact behavior below. |
| S3 / R2 | Bucket, region, optional custom endpoint, TLS, virtual-host style and optional key prefix; credentials can come from static AWS variables, profiles, instance metadata or role flows ([S3 docs](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/S3.md#L1-L35)). | Depsilo may use S3 internally, but it does not thereby expose an S3 API to clients. Implementing S3-compatible signing/API would be much larger than the WebDAV adapter. |
| Redis / Redis Cluster | `SCCACHE_REDIS_ENDPOINT` or cluster endpoints, optional username/password, DB, expiry and key prefix; TLS uses `rediss://` ([Redis docs](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/Redis.md#L1-L27)). | sccache connects directly using Redis protocol. It cannot point this backend at Depsilo's HTTP endpoint. |
| Memcached | Endpoint, optional credentials, expiration and key prefix ([docs](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/Memcached.md)). | Direct Memcached protocol; not an HTTP compatibility path. |
| GCS, Azure, GHA, OSS, COS | Provider-specific API and credentials; all are first-class storage choices in the configuration reference ([configuration](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/Configuration.md)). | Useful market context, but not required for Depsilo client compatibility. |
| Multi-level | Ordered fast-to-slow levels, read fallback, automatic backfill and write-through ([design](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/docs/MultiLevel.md#L13-L73)). | A build node can use local disk as L0 and Depsilo WebDAV as a shared slower level. |

There is no standalone generic `http` storage backend in the stable backend
list. The HTTP integration intended for a service such as Depsilo is named
`webdav`.

## Exact WebDAV wire contract

### Why dependency source is part of the evidence

sccache v0.15.0 delegates WebDAV to OpenDAL and pins OpenDAL 0.55.0
([lockfile](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/Cargo.lock#L1900-L1905)).
The sccache adapter passes endpoint, root/key prefix, username/password and
token directly into OpenDAL's WebDAV builder
([adapter source](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/webdav.rs#L20-L43)).
The method/header details below are therefore verified against the pinned,
official Apache OpenDAL v0.55.0 source, not inferred from generic WebDAV
documentation.

### Authentication

- A configured token becomes `Authorization: Bearer <token>`; username and
  password become Basic authentication
  ([OpenDAL builder](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/backend.rs#L147-L159)).
- This aligns with Depsilo's existing bearer machine credentials. Depsilo does
  not need Basic authentication for stock sccache compatibility.
- TLS remains essential because a bearer token and compiled artifacts are both
  exposed if plain HTTP crosses an untrusted network.

### Keys and object paths

sccache uses BLAKE3 and hex-encodes the digest
([digest source](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/util.rs#L42-L65),
[lowercase hex encoding](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/util.rs#L353-L366)).
The result is a 64-character lowercase hexadecimal key. Current `main` also has
an explicit regression assertion that the BLAKE3 hex digest is 64 characters
([test](https://github.com/mozilla/sccache/blob/7cd4f2c396a4712fe7627d5209083b7127841296/src/util.rs#L1897-L1905)).

Every remote key is normalized from `abcdef...` to
`a/b/c/abcdef...`: the first three characters each become a path segment and
the final segment remains the **full** 64-character key
([normalizer](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/utils.rs#L19-L22)).
Both reads and writes unconditionally apply that normalizer
([remote storage source](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/cache.rs#L205-L225),
[raw writes](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/cache.rs#L301-L343)).

An endpoint also receives a special `.sccache_check` object. On daemon startup,
sccache reads it, then attempts to write `Hello, World!` to determine whether
the backend is read-write or effectively read-only
([check logic](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/cache.rs#L227-L270)).

### Methods and response expectations

Normal cache reads use `GET`; OpenDAL accepts `200 OK` and `206 Partial
Content`. Its generic reader can send `Range`, although sccache's normal full
object `operator.read()` path does not request a range
([read implementation](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/backend.rs#L233-L247),
[GET construction](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/core.rs#L131-L156)).

Writes use one-shot `PUT` and always pass the buffered byte length, so the
request carries `Content-Length`. `200`, `201` and `204` are accepted
([writer](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/writer.rs#L54-L69),
[PUT construction](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/core.rs#L158-L192)).

However, a write is **not** merely a `PUT`. Before returning a writer, OpenDAL
ensures the parent collection exists
([write path](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/backend.rs#L250-L257)).
That process:

1. sends `PROPFIND` with `Depth: 0` and expects a WebDAV multistatus body for
   existing collections
   ([stat request](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/core.rs#L87-L129));
2. walks missing parents and sends `MKCOL`
   ([recursive creation](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/core.rs#L300-L330),
   [MKCOL request](https://github.com/apache/opendal/blob/48c48b1a1d3821af0864adc878e3864019ee9755/core/src/services/webdav/core.rs#L333-L365)).

`HEAD`, `OPTIONS`, conditional headers and locking are not part of sccache's
normal get/put path. OpenDAL exposes deletion, copy, rename and listing as
general WebDAV capabilities, but sccache's ordinary cache flow shown above only
needs startup check, `GET`, the parent `PROPFIND`/`MKCOL` sequence and `PUT`.

### Stored value format

The service may treat values as opaque bytes. sccache serializes an entry as a
ZIP archive; each member is declared as ZIP "stored" while its bytes are
zstd-compressed. It includes compiler outputs and optional stdout/stderr
([reader](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/cache_io.rs#L72-L128),
[writer](https://github.com/mozilla/sccache/blob/c2e4b4ca035f91f8b63978afc8e2c71f174fe306/src/cache/cache_io.rs#L172-L263)).
Depsilo does not need to parse this format, but it must not rewrite or
recompress the payload.

## Authentication, namespace and read/write semantics

sccache does not define a first-class multi-tenant namespace object for remote
storage. Isolation is assembled from backend facilities:

- WebDAV endpoint plus `key_prefix`;
- S3 bucket plus key prefix;
- Redis DB plus key prefix;
- provider credentials and ACLs.

For Depsilo, the endpoint path should remain the authoritative namespace and
the bearer credential should be bound to that namespace server-side. This is
stronger than relying on a client-selected prefix.

Stable v0.15.0 probes read and write access at startup and falls back to a
read-only cache mode when the write probe fails. After v0.15.0, current `main`
adds explicit `*_RW_MODE=READ_ONLY`/`READ_WRITE` configuration across remote
backends, including `SCCACHE_WEBDAV_RW_MODE`
([current configuration](https://github.com/mozilla/sccache/blob/7cd4f2c396a4712fe7627d5209083b7127841296/docs/Configuration.md#L228-L301),
[WebDAV note](https://github.com/mozilla/sccache/blob/7cd4f2c396a4712fe7627d5209083b7127841296/docs/Webdav.md#L10-L20)).
This explicit WebDAV setting is **not present in v0.15.0**, so documentation and
generated snippets must be version-aware.

Client-side read-only mode is useful for avoiding attempted writes, but it is
not an authorization boundary. Depsilo must continue enforcing `readonly` and
`readwrite` on the bearer credential.

## Pre-implementation Depsilo compatibility gap

The present Depsilo endpoint implements stock ccache's HTTP storage contract,
not sccache WebDAV. Its parser accepts only ccache's 33-character legacy key or
40-character modern key and only a flat or two-character subdirectory layout
([local parser](../../internal/compilecache/key.go#L32-L65)). Its handler accepts
`HEAD`, `GET`, `PUT` and `DELETE`, requires `Content-Length` on `PUT`, and
rejects other methods
([local handler](../../internal/api/ccache.go#L20-L114),
[PUT handling](../../internal/api/ccache.go#L146-L177)).

| Contract item | sccache v0.15.0 | Current Depsilo | Result |
|---|---|---|---|
| Authentication | `Authorization: Bearer` supported | Bearer credential | Compatible |
| Upload framing | Buffered `PUT` with `Content-Length` | Requires `Content-Length` | Compatible |
| Cache key | 64 lowercase hex | 33 legacy or 40 modern ccache characters | **Blocked** |
| Object path | `a/b/c/<full-64-key>` | flat or `<first-2>/<rest>` | **Blocked** |
| Startup object | GET then PUT `.sccache_check` | rejected as invalid ccache key | **Blocked** |
| Parent discovery/creation | `PROPFIND Depth:0`, then possibly `MKCOL` | method not allowed | **Blocked** |
| Payload | opaque sccache ZIP/zstd bytes | opaque object stream | Compatible if kept separate |
| Read-only | auto-detected in v0.15; explicit on current main | server-side credential permission | Compatible after adapter; server permission remains authoritative |

The official phrase "Ccache HTTP storage backend" in sccache's WebDAV document
should not be interpreted as automatic compatibility with every server that
implements only ccache's minimal GET/PUT contract. The pinned WebDAV client has
the additional path and collection behavior above. It also does not imply that
ccache and sccache can cross-hit the same entry.

## Recommended Depsilo design

Add a protocol-specific adapter rather than weakening the existing ccache
parser:

```text
/ccache/v1/{namespace}/...   -> existing ccache adapter (33/40-char keys)
/sccache/v1/{namespace}/...  -> new narrow WebDAV adapter (64-char keys)
                                  \
                                   -> shared credential/quota/LRU/storage core
```

The sccache adapter should:

1. Accept only `.sccache_check`, canonical `a/b/c/<full-64-lower-hex>` objects,
   and the three virtual parent collection paths needed for those objects.
2. Implement `GET` and length-delimited `PUT` with the status codes accepted by
   OpenDAL.
3. Implement only the necessary WebDAV subset: `PROPFIND Depth:0` with a valid
   `207 Multi-Status` response and idempotent/synthetic `MKCOL`. Do not expose
   arbitrary filesystem browsing, recursive listing, copy or rename.
4. Store sccache payloads opaquely in a protocol-separated object prefix. Expand
   metadata key storage to 64 characters without changing ccache validation.
5. Reuse Depsilo bearer credentials and bind each token to exactly one
   namespace. Enforce read-only server-side; do not trust only
   `SCCACHE_WEBDAV_RW_MODE`.
6. Generate both v0.15-compatible and current-main configuration snippets in
   the Admin UI. Do not advertise `rw_mode` in a v0.15 snippet.
7. Record protocol (`ccache` versus `sccache`) in metrics and audit data so
   capacity and hit-rate reports remain explainable.

Example stable v0.15.0 client configuration after that adapter exists:

```toml
[cache.webdav]
endpoint = "https://depsilo.example.com/sccache/v1/linux-ci"
token = "depsilo_cc_..."
```

Equivalent environment configuration:

```bash
export SCCACHE_WEBDAV_ENDPOINT='https://depsilo.example.com/sccache/v1/linux-ci'
export SCCACHE_WEBDAV_TOKEN='depsilo_cc_...'
```

For a source build from the inspected current `main`, a read-only worker can
also set:

```bash
export SCCACHE_WEBDAV_RW_MODE=READ_ONLY
```

## Required compatibility tests

Before claiming support, run a black-box test against the official v0.15.0
binary:

1. Start the sccache daemon and verify the `.sccache_check` GET/PROPFIND/PUT
   sequence succeeds.
2. Compile a C/C++ fixture, clear the node's local cache, compile again and
   verify a remote hit.
3. Repeat with Rust to verify that the service remains payload-agnostic.
4. Verify a read-only token can hit but cannot populate or overwrite an entry.
5. Verify namespace mismatch, revoked/expired token, malformed 64-character
   key, over-limit upload and corrupted cached value fail safely.
6. Repeat against a pinned post-v0.15/current-main build because remote
   read/write configuration changed after the stable release.
7. Keep the existing real-ccache suite unchanged to prove the new adapter does
   not regress `/ccache/v1`.

## Decision

**Proceed with sccache support as a separate compatibility adapter.** The
product value is high and the missing wire surface is bounded, but the feature
must not be described as already compatible, as a generic WebDAV server, or as
cross-client cache sharing between ccache and sccache.
