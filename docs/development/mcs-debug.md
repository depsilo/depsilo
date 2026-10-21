# MCS debug deployment

## Authorization and scope

On 2026-10-20, the user authorized deploying this repository's debug service to
`mcs:/data/dev_server/depsilo` and automatically upgrading that service after
future code changes. Once implementation and the required checks are complete,
update this environment as part of delivery without asking for deployment
permission again. A later instruction to skip deployment takes precedence.

This authorization covers the existing debug instance, including the restart
needed for an upgrade. It does not authorize scheduled upgrades, changes to
other environments, or resetting this instance's data or credentials. No
periodic upgrade job is configured by this instruction.

## Environment

| Setting | Value |
| --- | --- |
| SSH target | `mcs` (`nxd@192.168.50.102`) |
| Platform | Linux `amd64`, Docker Compose |
| Deployment directory | `/data/dev_server/depsilo` |
| Compose project / container | `depsilo-dev-server` |
| Compose service | `depsilo` |
| Port mapping | Host `23333` → container `23333` |
| Container user | `1000:1000` |
| Restart policy | `unless-stopped` |
| Bind mount | Deployment directory at the same container path |
| Server arguments | `serve --config /data/dev_server/depsilo/config.toml` |
| Service URL | `http://192.168.50.102:23333` |
| Admin URL | `http://192.168.50.102:23333/admin` |

The deployment directory contains `start.sh`, `status.sh`, `stop.sh`,
`logs.sh`, and `setup-token.sh`. Read their current contents and the deployed
Compose configuration before operating the service; they are the authority for
the installed command and artifact paths.

The instance has completed initialization. Keep its existing configuration,
database, signing key, cache, and ownership. `setup-token.sh` is for an instance
that needs setup; an ordinary upgrade continues using the initialized instance.

## Upgrade procedure

1. Complete the checks required by the [testing guide](testing.md). Record the
   source revision or working-tree provenance of the build being deployed.
2. Inspect the remote service status, current image, configuration paths, and
   readiness. Stage the replacement Linux `amd64` artifact while retaining the
   previous image and deployment configuration for rollback. Use a unique
   `depsilo-dev-server:dev-<UTC timestamp>` image tag. Store the source snapshot,
   its checksum, build logs, and `deployment.json` in
   `releases/<build-id>/` under the deployment directory.
3. Before replacing the running version or applying database migrations, stop
   the service with `docker compose stop -t 60 depsilo` and take a consistent
   backup in `backups/<build-id>/` (directory mode `0700`, files `0600`). Preserve
   the database, configuration, signing key, and cache together, including their
   ownership and permissions. The current cold-backup archive contains
   `compose.yaml`, `config.toml`, all of `data/`, and the management scripts.
   Save the previous image tag/ID alongside it; verify the archive checksum and
   SQLite integrity before starting the new version. Serialize deployments with
   `flock` on `.deploy.lock`.
4. Replace only the existing service's `image` in `compose.yaml`, then run
   `docker compose up -d --no-deps --wait --wait-timeout 90 depsilo`.
   Preserve its bind mount, port, restart policy, and container user.
5. Confirm that the container remains running and
   `http://192.168.50.102:23333/ready` returns HTTP 200 with `status: "ready"`.
   Check the reported version against the deployed build; for non-unique
   development version strings, also verify the running image identity. Review
   startup logs and check the changed user flow.
6. Report the deployed build, readiness and flow-check results, and rollback
   artifact location. If deployment cannot complete, report the blocking
   condition and the version left running rather than claiming an upgrade.

If the new version fails validation, stop it before rollback. Restore the
previous image and configuration; if startup migrated persistent state, restore
the matching pre-upgrade backup as well. Repeat the readiness and version checks
after rollback. Keep the failed deployment's logs available for diagnosis.
Schema migrations are forward-only: an older image cannot open a newer schema.
Retain the failed version's state separately before restoring a cold backup.

### Building with the current worktree

Include tracked modifications and new source files in the snapshot; a
`git archive HEAD` omits uncommitted fixes. Exclude local config, credentials,
runtime data, build output, and dependency directories. Record `HEAD`, whether
the tree is dirty, and the source archive's SHA-256 in `deployment.json`; stamp
the binary version and commit accordingly.

Prefer the repository Dockerfile. The host could not reach `proxy.golang.org`
during the initial upgrade. If that recurs, build the production frontend
locally and cross-compile `./cmd/depsilo` with
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`, using verified local dependencies.
Stage the binary and license files, then use the current Dockerfile's final
runtime stage on `mcs`, adjusting only its `COPY --from=backend` source paths.
This retains the pinned base image, certificate setup, user, and healthcheck.
Keep symbols in debug builds. Verify the binary checksum after transfer and
run the candidate image's `version` command before stopping the old service.

### Operations and validation

```bash
ssh mcs 'cd /data/dev_server/depsilo && ./status.sh'
ssh mcs 'cd /data/dev_server/depsilo && ./logs.sh'
ssh mcs 'cd /data/dev_server/depsilo && docker compose exec -T depsilo depsilo version'
curl --noproxy '*' --fail http://192.168.50.102:23333/ready
```

Use browser navigation or `Accept: text/html` when checking Admin document
routes; requests without a browser Accept header deliberately receive a 404.
Check that setup remains complete and anonymous Admin API requests still
receive 401. Compare configuration, signing-key, user, and API-token
fingerprints against the pre-upgrade state without displaying secrets.

When local browser permissions prevent LAN access, use a temporary SSH
loopback tunnel for browser smoke checks and close it afterward. Also check
the LAN URL with `curl`; a tunnel alone does not verify direct LAN access.
Report authenticated flow coverage separately when no existing login is
available.

## Secret handling

Keep credentials, signing keys, configuration contents containing secrets, and
backup archives on the deployment host or another protected location outside
the repository. Never commit them or print their contents in deployment reports.
Signing keys must remain paired with the database; see the
[quick start](quick-start.md#start-the-normal-development-service) for the
managed key and backup semantics.
