# Deployment defaults

Depsilo keeps a zero-config installation under one state root. Persist and back
up that root in every deployment; S3 can move object data elsewhere, but the
generated configuration and SQLite database remain local.

## Default paths

| State | Binary | Official container |
| --- | --- | --- |
| State root | `~/.depsilo` | `/root/.depsilo` |
| Configuration | `~/.depsilo/config.toml` | `/root/.depsilo/config.toml` |
| SQLite database | `~/.depsilo/data/depsilo.db` | `/root/.depsilo/data/depsilo.db` |
| Package cache | `~/.depsilo/data/cache` | `/root/.depsilo/data/cache` |
| Compiler cache | `~/.depsilo/data/compile-cache` | `/root/.depsilo/data/compile-cache` |

The setup flow creates the state directory and writes its generated
`config.toml` with absolute database and local-cache paths. The compiler cache
path is reserved under the same root but remains disabled until configured.

For Docker and Compose, persist `/root/.depsilo` with one named volume. This
keeps the generated configuration, administrator and user records, policy
state, SQLite database, and local caches across restart and container
recreation.

## Overrides

Advanced deployments can move each component independently:

- `DEPSILO_CONFIG` selects a custom configuration file.
- `DEPSILO_DATABASE_DSN` selects a custom SQLite path.
- `DEPSILO_STORAGE_PATH` selects a custom local package-cache path.
- `DEPSILO_COMPILE_CACHE_STORAGE_PATH` selects a custom compiler-cache path.
- `[storage] type = "s3"` and `[compile_cache.storage] type = "s3"` move object
  data to S3-compatible storage; the configuration and SQLite database still
  need durable local storage.

Explicit settings keep their existing precedence:

```text
CLI flag → DEPSILO_* environment → config file → built-in default
```

## Health checks

```bash
depsilo --version
depsilo status
depsilo doctor
curl -fsS http://127.0.0.1:23333/ready
```

For Docker or Compose, use the binary's stable path inside the container:

```bash
docker exec depsilo /app/depsilo status
docker exec depsilo /app/depsilo doctor
```

See [`config.example.toml`](../config.example.toml) for the complete schema and
[Admin control plane](admin-control-plane.md) for runtime ownership and restart
semantics.
