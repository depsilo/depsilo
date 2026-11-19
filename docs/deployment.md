# Deployment

Choose a container or binary installation. A first run needs no repository
checkout, configuration file, or database and cache environment variables.
This guide follows the current branch; when operating a tagged release, use
the documentation bundled with that release.

Depsilo keeps a zero-config installation under one state root. Persist that
root in every deployment; S3 can move object data elsewhere, but the generated
configuration and SQLite database remain local. Back up the configuration and
the database with `depsilo backup`;
[Back up and restore](#back-up-and-restore) lists what the archive leaves out.

## Docker

```bash
docker run -d \
  --name depsilo \
  --publish 23333:23333 \
  --volume depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest
docker logs depsilo
```

Open <http://127.0.0.1:23333>: package routes and the client guide work
right away. To use Admin, create the administrator at
<http://127.0.0.1:23333/admin> ([first-run setup](#first-run-setup)).
`docker logs depsilo` prints the startup output, which includes the bootstrap
token that page asks for.

`--publish 23333:23333` publishes the port on every interface of the Docker
host, and package routes need no sign-in: every machine that can reach the
port can download through Depsilo. Publish it this way only on a network you
trust. For a trial on a single machine, use
`--publish 127.0.0.1:23333:23333` instead, which exposes the port only on the
Docker host. On a Linux host, Docker routes the traffic of a published port
before the rules of a host firewall such as ufw apply; Docker documents this.
Limit access with the publish address, `--publish <address>:23333:23333`, and
not with the host firewall alone.

For a long-lived deployment, replace `latest` with a full `X.Y.Z` release tag
or immutable image digest. GHCR is the canonical registry; Docker Hub is a
mirror. See [release verification](release-verification.md) for signed release
artifacts and image verification. The named volume preserves configuration,
SQLite, and local caches across container recreation. For host-mounted state
or an older deployment, use the corresponding [state procedure](#default-paths).

## Docker Compose

The repository's [`compose.yaml`](../compose.yaml) contains one service, one
published port, and one persistent named volume. Download it into a dedicated
deployment directory, or use the copy from your selected release:

```bash
curl -fsSLO https://raw.githubusercontent.com/depsilo/depsilo/master/compose.yaml
docker compose up -d
docker compose exec depsilo depsilo setup token
```

The download above tracks `master`; replace `master` with the release tag when
pinning the deployment. Compose accepts these environment overrides:

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEPSILO_IMAGE` | `ghcr.io/depsilo/depsilo:latest` | Pinned release or locally built image |
| `DEPSILO_BIND_ADDRESS` | `0.0.0.0` | Host interface on which to publish the port |
| `PORT` | `23333` | Published host port; the container still listens on `23333` |

The default `DEPSILO_BIND_ADDRESS` publishes the port on every host interface,
with the consequence described under [Docker](#docker). Set it to `127.0.0.1`
for a trial on a single machine.

For example, after building the local image, recreate the container with:

```bash
DEPSILO_IMAGE=depsilo/depsilo:local \
DEPSILO_BIND_ADDRESS=127.0.0.1 \
PORT=18080 \
docker compose up -d --force-recreate
```

Keep these values in the deployment's `.env` file or export them for subsequent
Compose commands. Changing the image variable does not replace a running
container until you recreate it. Use the published host port in your browser;
the startup log reports the container listener.

For a host directory instead of a named volume, download the alternative
template and follow [Bind-mounted state](#bind-mounted-state) to prepare its
ownership before starting it:

```bash
curl -fsSLO https://raw.githubusercontent.com/depsilo/depsilo/master/compose.bind.yaml
```

Use `-f compose.bind.yaml` consistently for that deployment, including token
retrieval: `docker compose -f compose.bind.yaml exec depsilo depsilo setup token`.

## Binary installation

On Linux or macOS:

```bash
curl -fsSL https://depsilo.com/install.sh | bash
depsilo serve
```

The installer detects the OS and CPU architecture, downloads the release
archive and `checksums.txt`, and verifies the archive's SHA-256 before installing
the binary. It needs Bash, `curl` or `wget`, an archive extractor, and
`sha256sum` or `shasum`. It does not start Depsilo or register an operating-system
service. `serve` runs in the foreground; stop it with Ctrl+C.

If `curl` reports `403` or another error, nothing is installed even though the
pipeline's exit status is 0; download `install.sh` or the archive from
[GitHub Releases](https://github.com/depsilo/depsilo/releases) instead.

Alternatively, download the appropriate archive from
[GitHub Releases](https://github.com/depsilo/depsilo/releases), verify it using
[release verification](release-verification.md), and put the extracted binary
on your `PATH`. Windows users can use the native executable from its release
archive.

**Listen address.** The binary listens on `127.0.0.1` by default, so only the
machine itself can reach it. First-run setup does not change that: it writes
`host = "127.0.0.1"` under `[server]` in `config.toml`. To serve other
machines:

1. Complete [first-run setup](#first-run-setup) on the machine itself. The
   bootstrap token and the new administrator password then never cross the
   network. On a server without a browser, use
   [Scripted setup](#scripted-setup). Or forward the port from your
   workstation with `ssh -L 23333:127.0.0.1:23333 <server>` and open
   `http://127.0.0.1:23333` there.
2. Listen on the one address that faces your clients, not on every interface
   with `0.0.0.0`. Pass the address with `--host`, or set
   `DEPSILO_SERVER_HOST`, at every start:

   ```bash
   depsilo serve --host '<server-address>'
   ```

   The flag overrides the variable, and both override `config.toml`. To make
   the address permanent, change `host` under `[server]` in `config.toml` and
   restart. An IPv6 address works with or without square brackets in all
   three places, for example `--host '::1'` or `--host '[::1]'`; the startup
   summary prints it in brackets, as in `http://[::1]:23333`.

   Depsilo then no longer answers on `127.0.0.1`. `depsilo status`,
   `depsilo doctor`, `depsilo init-agent` and the `curl` checks in this guide
   use the loopback address unless told otherwise: set
   `DEPSILO_URL=http://<server-address>:23333` for the commands (an IPv6
   address in brackets, `http://[<address>]:23333`), and put the same address
   into the `curl` checks.
3. In the host firewall, allow the port only from the network your clients are
   on.
4. For Admin access from other machines, put a
   [reverse proxy with TLS](#reverse-proxy-and-tls) in front of Depsilo. Over
   plain HTTP the administrator password crosses the network unencrypted.

Steps 2 and 3 let package clients reach Depsilo directly, over plain HTTP on a
network you trust. Skip them when every client goes through a reverse proxy
on the same machine: leave Depsilo on `127.0.0.1` then, and let package
clients and Admin both use the proxy's address.

**Without sign-in.** Sign-in protects Admin and its API. Package routes,
`/metrics` and `/api/v1/stats` answer every client that can reach the port, and
`/api/v1/stats` includes the names and URLs of the configured Upstreams. The
listen address and the firewall are what limit access to them; the
[threat model](threat-model.md#4-trust-boundaries) explains why package routes
are open.

### Installer options

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEPSILO_VERSION` | `latest` | Exact release tag to download, including its `v` prefix |
| `DEPSILO_INSTALL_DIR` | `/usr/local/bin` | Directory in which to install the binary |

Replace `vX.Y.Z` with the desired release tag. Pass the variables to the Bash
process running the installer, rather than only to the download command:

```bash
curl -fsSL https://depsilo.com/install.sh | \
  DEPSILO_VERSION=vX.Y.Z DEPSILO_INSTALL_DIR="$HOME/.local/bin" bash
```

Ensure the chosen directory is on your `PATH`. The default `/usr/local/bin`
installation uses `sudo` when that directory is not writable; a custom
directory must be writable by the invoking user.

### Background process

```bash
depsilo start --daemon
depsilo status
depsilo stop
```

Run `stop` when you want to shut down the daemon; it can run from a later,
independent terminal under the same user. Linux and macOS detach from the
launching terminal. Windows uses a per-start shutdown event. Daemon mode does
not configure automatic startup at boot or restart after a crash.

`start --daemon` accepts the same listener and configuration flags as `serve`,
for example `depsilo start --daemon --config /etc/depsilo.toml --port 18080`.
It waits for readiness and prints the log location. The daemon record and log
normally live at `~/.local/share/depsilo/depsilo.pid` and
`~/.local/share/depsilo/depsilo.log`, separate from the application state root.
Each new daemon start replaces the previous log. For a custom port, select the
matching URL when running [health checks](#health-checks).

### Upgrade a v0.9.0 daemon

Before replacing a v0.9.0 binary running in daemon mode, stop it with the
v0.9.0 binary. Starting with v0.9.1, daemon control requires a process-identity
record and deliberately refuses the older PID-only file. If the binary has
already been replaced, first verify that the recorded process has ended, then
remove `~/.local/share/depsilo/depsilo.pid` manually before starting the new
binary.

## Tray application

`depsilo-tray` is a menu-bar application for a desktop machine. It runs the
complete Depsilo server inside its own process and adds a menu with a status
line and the entries **Open Admin Panel**, **Open Portal**, **Run Doctor** and
**Quit Depsilo**. Quitting the application stops the server.

Each release carries two tray bundles, and each has its own `.sigstore.json`
bundle for [release verification](release-verification.md):

| Asset | Platform | Notes |
| --- | --- | --- |
| `depsilo-tray-linux-amd64.tar.gz` | Linux, amd64 | Needs a desktop with a system tray. |
| `depsilo-tray-macos.zip` | macOS 11 or later, Apple Silicon | Contains `Depsilo.app`. The application is not code-signed, so macOS refuses the first launch. On macOS 15 or later, try to open it once, then choose **Open Anyway** under System Settings → Privacy & Security. On macOS 11 to 14, right-click the application and choose **Open**. It has no Dock icon. |

There is no tray bundle for Windows or for Linux on arm64.

**Install on Linux.** The archive holds four files side by side, without a
top-level directory. Extract it into a directory of its own and run the
installer as a file from there; it needs no `sudo`:

```bash
mkdir depsilo-tray
tar xzf depsilo-tray-linux-amd64.tar.gz -C depsilo-tray
bash depsilo-tray/install-linux.sh install
```

The installer copies the binary to `~/.local/bin/depsilo-tray` and a launcher
to `~/.local/share/applications/depsilo-tray.desktop`. It installs the icon
only when `rsvg-convert`, ImageMagick or Inkscape is available. Start
**Depsilo** from the application menu, or run `~/.local/bin/depsilo-tray` in
a terminal. The installer ends with a note for GNOME on Wayland: the tray
icon needs the "AppIndicator and KStatusNotifierItem" extension there.

The same script takes three more commands:

| Command | Effect |
| --- | --- |
| `bash depsilo-tray/install-linux.sh autostart-enable` | Start the application at login, through `~/.config/autostart/depsilo-tray.desktop` |
| `bash depsilo-tray/install-linux.sh autostart-disable` | Remove that entry |
| `bash depsilo-tray/install-linux.sh uninstall` | Remove the installed files |

**State and port.** The tray application reads the same configuration and
keeps the same state as the binary: `~/.depsilo`, as listed under
[Default paths](#default-paths), and the listener `127.0.0.1:23333`. It
therefore cannot run at the same time as `depsilo serve` for the same user.
It always starts its own server; when the port is taken, it exits. The menu
links and the status line use `DEPSILO_URL`, then `http://localhost` with the
port in `DEPSILO_PORT`, and otherwise `http://localhost:23333`. They do not
follow a port changed in `config.toml` or with `DEPSILO_SERVER_PORT`.

**Bootstrap token.** The tray bundles contain no `depsilo` command and the
menu has no entry for the token, so `depsilo setup token` is not available
from a tray-only installation. While setup is pending, read the token from
the file that the server keeps for that purpose:

```bash
cat ~/.depsilo/data/secrets/bootstrap.token
```

The file is readable only by its owner, is rewritten at every start, and is
removed when setup is complete. Then choose **Open Portal** and continue with
[first-run setup](#first-run-setup).

**Logs.** The application writes no log file. Its log and the startup summary
go to standard error, so they are visible only when you start the binary from
a terminal.

## First-run setup

The startup summary reports server, database, and cache readiness and the
Portal URL. For an unconfigured instance it also shows the generated bootstrap
token, or notes that a token was supplied through `DEPSILO_BOOTSTRAP_TOKEN`.
For a container, `docker logs depsilo` shows the summary. Retrieve the current
token while the service is running:

```bash
depsilo setup token
```

Run this as the service user with the same configuration path and listener
environment. For a custom file, use `depsilo setup token --config PATH` (or
`DEPSILO_CONFIG`). This command uses the local configuration and intentionally
does not follow `DEPSILO_URL` to a remote instance.

Without flags, the command asks the listener named by `config.toml`,
`DEPSILO_SERVER_PORT` and `DEPSILO_SERVER_HOST`, which is `127.0.0.1:23333` by
default. For an instance started on another port or address, name it with
`--port` (or `-p`) and `--host`; otherwise the answer comes from whatever
listens on the default port:

```bash
depsilo setup token --port 18080
depsilo setup token --port 18080 --host '<server-address>'
```

When the port is not `23333`, the startup summary prints the command with
`--port` filled in. When Depsilo listens on an address that `127.0.0.1` does
not reach, such as `::1` or one specific address, it adds `--host` as well.

In containers, run the command inside the running service:

```bash
docker exec depsilo depsilo setup token
```

For Compose, use `docker compose exec depsilo depsilo setup token`. Paste the
command's output into the setup form. `jwt.key` is the persistent signing key,
not the bootstrap token; token retrieval stops after setup is complete.

A generated bootstrap token changes every time Depsilo starts, until setup is
complete; print it again after a restart. A token preset through
`DEPSILO_BOOTSTRAP_TOKEN` stays the same across restarts.

**Before setup.** Setup is needed only for Admin. An instance without
`config.toml` serves every ecosystem the setup page offers, from the Upstreams
the page proposes (regional mirrors first, then the official registry), and
the Portal, its client guide, the agent prompts, and `depsilo init-agent`
work as on a configured instance. Admin and sign-in wait for setup, which
starts when you open `/admin`. Finishing setup replaces those Upstreams with the ones
in the form when the service restarts. Cached files stay when the cache path
is unchanged, and the form proposes the port already in use.

1. Open `/admin` at the address exposed by your deployment and enter the
   bootstrap token, unless the page does not ask for it (see below).
2. Create the first administrator. There is no default administrator password.
3. Continue to the client connection guide, choose a package manager, and run
   the configuration and test commands on the client machine.

**No token on the server itself.** A setup request that reaches Depsilo
directly from a loopback address (`127.0.0.1` or `::1`) and carries no
forwarding header (`Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`,
`X-Forwarded-Proto`, `X-Real-IP`, `Via`) needs no bootstrap token: it comes
from someone on the server, so the page does not show the token field. This
applies only to a generated token. Through a container's published port the
request comes from the runtime's gateway, so the token stays required there.
Set `DEPSILO_BOOTSTRAP_TOKEN` to require the token from everyone, for example
on a host shared with other accounts, or behind a reverse proxy on the same
host that forwards without any of those headers.

Opened over plain HTTP from another machine, the setup page warns that the
bootstrap token and the new password are sent unencrypted. Finish setup on the
server itself, or over a network you trust.

**Reopened setup.** When a configuration file exists but no local
administrator can sign in, Depsilo reopens setup. This happens when you start
a hand-written `config.toml` on an empty database, or when the database was
lost or replaced. The startup summary then says `Setup reopened:` instead of
`First-time setup:`, and the setup page shows a notice that the instance
already has a configuration file.

Finishing a reopened setup replaces `config.toml` with a generated file. The
generated file keeps the database location and the signing key, and takes the
port, the cache path and the Upstreams from the setup form. Other settings are
not carried over, so sections such as `[egress]` and a changed cache size are
gone. Before replacing the file, Depsilo copies it beside itself as
`config.toml.pre-setup-<UTC timestamp>.bak`, for example
`config.toml.pre-setup-20261116T101500Z.bak`, with mode `0600`, and logs
`kept the previous config before setup replaces it` with the path in
`backup_path`. If the copy cannot be written, setup stops and `config.toml`
stays as it was.

After a reopened setup, copy the settings you still need from the `.bak` file
into the new `config.toml` and restart. Depsilo never deletes the copy. It
holds whatever secrets the old file held, so remove it when you are done. To
start a hand-written `config.toml` without the setup page, see
[Scripted setup](#scripted-setup).

### If the first run goes wrong

- **`depsilo serve` exits and reports that the port is in use.** The line is:

  ```text
  Port 23333 is already in use. Another Depsilo may be running (try "depsilo status"), or choose another port with --port.
  ```

  Another process holds the port. Run the status command that the line
  suggests to see whether it is a Depsilo. Stop that process, or start this
  one with `--port`. `depsilo start --daemon` prints the same line, followed
  by a line that names the daemon log:

  ```text
  Error: daemon exited before readiness: exit status 1 (see <home>/.local/share/depsilo/depsilo.log)
  ```

- **Startup stops with one `Error:` line.** Depsilo refuses a setting or an
  environment variable it cannot use and names it, for example:

  ```text
  Error: egress.upstream_proxy: must be an http:// or https:// proxy URL with a host
  ```

  Correct the named setting in `config.toml`, or the variable that sets it,
  here `DEPSILO_EGRESS_UPSTREAM_PROXY`, and start again. `depsilo serve`
  exits with 1. `depsilo start --daemon` prints the same line.
- **The page does not open from another machine.** The binary listens on
  `127.0.0.1` unless you tell it otherwise; follow the steps after
  **Listen address** under [Binary installation](#binary-installation). For
  a container, check with `docker port depsilo` that the port is not
  published on `127.0.0.1` only. Then check the host firewall.
- **The bootstrap token is rejected.** The setup page marks the token field,
  and a scripted request gets `403` with the code `BOOTSTRAP_TOKEN_REQUIRED`.
  A missing, outdated and mistyped token all get this answer. A generated
  token changes at every start, so print the current one and paste it again.
  When the instance does not listen on port `23333`, pass `--port` to
  `depsilo setup token`, or the token may come from a different instance. A
  scripted request must send the token in the `X-Depsilo-Bootstrap-Token`
  header.
- **The setup page times out after submitting.** Depsilo saves the
  administrator and `config.toml`, answers the page, and restarts itself in
  place about a second later. The page then asks `/health` at the address in
  the browser once a second. After 30 seconds without an answer it shows
  `Could not reach the restarted service` and a **Retry connection** button.
  Setup is already saved at that point, so do not start over:

  1. Check that the process is still running, with `depsilo status` or
     `docker ps`, and read its log. Start it again if it has exited.
  2. If you changed the port under **Advanced settings**, the service now
     listens on the new port. Open the new address, or update the container
     port mapping or the proxy target.
  3. Choose **Retry connection**, or reload the page and sign in.

  On Windows, Depsilo cannot restart itself in place. The page shows
  `Configuration saved. Restart Depsilo to finish.` instead: stop the process,
  start it again, then choose **Retry connection**.
- **Startup fails with `Permission denied`.** The line names the failing
  operation and path, for example:

  ```text
  Permission denied: mkdir /root/.depsilo/data. The state directory must be writable by the user Depsilo runs as; the official container image runs as UID and GID 10001. See docs/deployment.md#bind-mounted-state
  ```

  The user that runs Depsilo cannot write the state directory. For a
  container this is usually a host directory that is not owned by
  `10001:10001`; with `--restart unless-stopped` the container keeps
  restarting until you fix it. Prepare the directory as described under
  [Bind-mounted state](#bind-mounted-state). For the binary, make `~/.depsilo`
  writable by the user that runs Depsilo.

  When a hand-written `config.toml` leaves the database or cache path
  relative, the ownership is not the cause. The line then says so and names
  the settings, for example in a container:

  ```text
  Permission denied: mkdir /app/data. Relative paths in config.toml resolve against the working directory, /app; set absolute paths in /root/.depsilo/config.toml for database.dsn, storage.path (a setting left out counts: its default is relative). See docs/deployment.md#default-paths
  ```

  Give those settings absolute paths as described under
  [Scripted setup](#scripted-setup).
- **Every Upstream is failing.** Clients get `502` with
  `all upstreams are unhealthy`, and Admin → Upstreams shows every Upstream
  as failed. The usual cause is the DNS of the host: proxy software in fake-IP
  mode whose TUN mode is off, or an overlay network. With `upstream_proxy`
  set, it can also be the proxy, when it does not run or cannot reach the
  Upstreams. Run
  `depsilo doctor` and follow
  [Upstream network egress](#upstream-network-egress).

## Scripted setup

Use this section to provision an instance without a browser. There are two
ways. The first sends the setup form's data in one request and suits a new
instance, including a container. The second starts a `config.toml` that you
wrote yourself and creates the administrator from environment variables. Run
every step on the server itself.

**Complete setup with one request.**

1. Create a bootstrap token and start Depsilo with it in
   `DEPSILO_BOOTSTRAP_TOKEN`. Depsilo refuses to start unless the value has at
   least 24 characters, no leading or trailing whitespace, and at least 8
   distinct characters. Use a random value of 32 bytes or more:

   ```bash
   umask 077
   openssl rand -hex 32 > bootstrap-token
   printf 'X-Depsilo-Bootstrap-Token: %s\n' "$(cat bootstrap-token)" \
     > bootstrap-token.header
   DEPSILO_BOOTSTRAP_TOKEN="$(cat bootstrap-token)" depsilo start --daemon
   ```

   For a container, put the variable in a file and pass the file, so that the
   value is not part of the command:

   ```bash
   printf 'DEPSILO_BOOTSTRAP_TOKEN=%s\n' "$(cat bootstrap-token)" > setup.env
   docker run -d \
     --name depsilo \
     --env-file setup.env \
     --publish 23333:23333 \
     --volume depsilo-data:/root/.depsilo \
     --restart unless-stopped \
     ghcr.io/depsilo/depsilo:latest
   ```

   The startup summary then reads
   `Bootstrap token: configured via DEPSILO_BOOTSTRAP_TOKEN` and does not print
   the value. A value that breaks the rules above stops the container at every
   start; with `--restart unless-stopped` it keeps restarting until you
   correct the value.

2. Write the request body to a file that only you can read, for example
   `setup.json`:

   ```json
   {
     "server": { "port": 23333 },
     "storage": { "path": "./data/cache" },
     "admin": { "username": "<admin-username>", "password": "<admin-password>" },
     "ecosystems": {
       "pypi": {
         "enabled": true,
         "upstreams": [
           { "name": "official", "url": "https://pypi.org", "priority": 1 }
         ]
       },
       "npm": {
         "enabled": true,
         "upstreams": [
           { "name": "official", "url": "https://registry.npmjs.org", "priority": 1 }
         ]
       }
     }
   }
   ```

   | Field | Meaning |
   | --- | --- |
   | `server.port` | Listen port written to `config.toml`, from 1 to 65535. Use the port the instance listens on now; in a container that is `23333`. |
   | `storage.path` | Directory of the package cache. Required. A relative path is resolved against the directory of `config.toml`, so `./data/cache` is the [default path](#default-paths). |
   | `admin.username` | 3 to 64 characters: letters, digits, `.`, `_` and `-`, starting with a letter or digit. |
   | `admin.password` | At least 12 characters from three of the four classes lower case, upper case, digit and other, or at least 20 characters. At most 72 bytes, and it must not contain the user name. Control characters and a few well-known passwords are refused. |
   | `ecosystems` | One entry per ecosystem to serve, keyed by `pypi`, `apt`, `npm`, `go`, `cargo`, `maven`, `rubygems`, `composer`, `nuget`, `conda`, `cran`, `alpine` or `helm`. At least one must have `"enabled": true`. |
   | `ecosystems.<name>.upstreams` | At least one Upstream for an enabled ecosystem. `name` is unique within the ecosystem, `url` is an `http://` or `https://` URL, and `priority` is a positive integer; lower numbers are tried first. An optional `proxy` is an HTTP(S) proxy URL for that Upstream. |

   The request configures only the ecosystems it lists; the default Upstream
   list of the setup page is not applied. Docker Registry and Hugging Face are
   not accepted here; add them afterwards as described under
   [Optional ecosystems](#optional-ecosystems).

3. Send the request to the loopback address. The token header and the body
   come from the two files, so neither appears on the command line:

   ```bash
   curl --silent --show-error --max-time 60 \
     --header 'Content-Type: application/json' \
     --header @bootstrap-token.header \
     --data-binary @setup.json \
     http://127.0.0.1:23333/api/v1/setup/complete
   ```

   For a container, run the same command on the Docker host; `127.0.0.1`
   there reaches the published port. Allow the request up to a minute: while
   the first blocklist sync is writing to the database, the answer can take
   up to about 45 seconds.

   When the Docker host has no suitable `curl`, or the port is not published
   there, send the request from inside the container. The image has BusyBox
   `wget` and no `curl`. The body stays on the host and is read from standard
   input:

   ```bash
   docker exec -i depsilo sh -c 'wget -q -O - --header "Content-Type: application/json" --header "X-Depsilo-Bootstrap-Token: $DEPSILO_BOOTSTRAP_TOKEN" --post-file /dev/stdin http://127.0.0.1:23333/api/v1/setup/complete' < setup.json
   ```

   For a container started without `DEPSILO_BOOTSTRAP_TOKEN`, replace
   `$DEPSILO_BOOTSTRAP_TOKEN` with `$(depsilo setup token)`; the generated
   token then never leaves the container. With Compose, the command starts
   with `docker compose exec -T depsilo sh -c` instead. On an HTTP error
   BusyBox `wget` exits with 1 and prints only the status line, for example
   `wget: server returned error: HTTP/1.1 403 Forbidden`, without the code and
   message.

   Without `--fail`, `curl` exits with 0 for an HTTP error as well, so a
   script must check the body. Success is `200` with this body:

   ```json
   {"message":"Configuration saved. Server restarting...","reconnect_url":"http://127.0.0.1:23333/","restart_strategy":"exec","status":"ok"}
   ```

   With `restart_strategy` `exec`, Depsilo restarts itself in place about a
   second later and loads the new `config.toml`. On Windows it is
   `supervisor_required`: restart the process yourself, then go on with
   step 4.

   `reconnect_url` is the address to use after the restart. A request whose
   `Host` names the port the instance listens on, without an
   `X-Forwarded-Proto` header, gets that host name with the port from
   `server.port`. Any other
   request, for example through a published host port or a reverse proxy,
   gets the address it was sent to. Its scheme follows
   `X-Forwarded-Proto`, as described under
   [Reverse proxy and TLS](#reverse-proxy-and-tls). These are the errors:

   | Status | Code | Meaning |
   | --- | --- | --- |
   | `400` | `INVALID_BODY`, `INVALID_PORT`, `INVALID_STORAGE_PATH`, `INVALID_ECOSYSTEM`, `INVALID_UPSTREAM`, `NO_ECOSYSTEM`, `INVALID_ADMIN_CREDENTIALS` | The body breaks a rule from the table above; `message` says which. |
   | `403` | `BOOTSTRAP_TOKEN_REQUIRED` | The header is missing or the token is not the current one. |
   | `409` | `ALREADY_CONFIGURED` | Setup is already complete. |
   | `409` | `ADMIN_CREDENTIALS_MISMATCH` | A local administrator already exists. Send that account's current user name and password. |
   | `500` | `ADMIN_CREATE_FAILED` | The administrator was not saved. Send the request again. |
   | `500` | `WRITE_FAILED` | The administrator is saved, but `config.toml` was not written. Fix the cause named in the log, then send the same request again. |

4. Wait for the restarted instance. Until the process restarts,
   `GET /api/v1/setup/status` answers `"restart_pending":true` beside
   `"needs_setup":false`, and package routes answer `404`. During the restart
   the connection is refused. Poll until an answer with status `200` has
   `needs_setup` false and no `restart_pending`. Count a refused connection,
   another status or a body that is not JSON as "not yet". With `jq`:

   ```bash
   until curl -fs --max-time 5 http://127.0.0.1:23333/api/v1/setup/status \
       | jq -e '.needs_setup == false and .restart_pending != true' >/dev/null
   do
     sleep 1
   done
   ```

   With `supervisor_required`, `restart_pending` stays until you restart the
   process. The restarted instance answers
   `{"needs_setup":false,"token_required":false}` and serves packages.
   `depsilo setup token` now reports that initial setup is already complete,
   and the administrator can sign in to Admin.

5. Delete `setup.json`, `bootstrap-token`, `bootstrap-token.header` and
   `setup.env`, and remove `DEPSILO_BOOTSTRAP_TOKEN` from the service's
   environment. A preset token is not replaced at restart, so the same value
   would be accepted again if setup is ever reopened. The restart after setup
   keeps the environment of the process, so the daemon from step 1 still has
   the variable: run `depsilo stop`, then `depsilo start --daemon` without
   it. For a container, remove it with `docker rm -f depsilo` and run it
   again without `--env-file`; the state volume keeps the instance.

**Start from your own configuration file.** `DEPSILO_ADMIN_USERNAME` and
`DEPSILO_ADMIN_PASSWORD` create the first administrator at startup, without a
bootstrap token. They apply only when a configuration file already exists and
no local administrator can sign in. On an instance without a configuration
file, which includes a new container of the official image, they are ignored:
setup stays pending and the startup summary says so:

```text
  DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD are ignored: they apply only
  when a configuration file exists, so they created no administrator. Finish setup
  in the Portal, or see docs/deployment.md#scripted-setup.
```

Write the configuration file first, for example as
`/var/lib/depsilo/config.toml`. Give the database and the cache absolute
paths:

```toml
[database]
dsn = "/var/lib/depsilo/data/depsilo.db"

[storage]
path = "/var/lib/depsilo/data/cache"

[[pypi.upstreams]]
name = "official"
url = "https://pypi.org"
priority = 1
```

The [default paths](#default-paths) under `~/.depsilo` apply only while no
configuration file exists. With a file that does not set these two paths, the
database and the cache are `./data/depsilo.db` and `./data/cache`, relative to
the directory the process is started from. A start from another directory, or
by a service manager, then finds an empty instance. The signing key is created
as `data/secrets/jwt.key` beside the configuration file. An ecosystem without
an `upstreams` entry in the file is not served.

Start Depsilo with the file and both variables, reading the password from a
file that only you can read:

```bash
DEPSILO_ADMIN_USERNAME='<admin-username>' \
DEPSILO_ADMIN_PASSWORD="$(cat admin-password)" \
depsilo start --daemon --config /var/lib/depsilo/config.toml
```

The user name and the password follow the same rules as `admin.username` and
`admin.password` above; Depsilo refuses to start with a password that breaks
them. Sign in to confirm, then remove both variables from the service's
environment and delete the password file. The variables never change an
administrator that already exists.

For a container, write the file with the two paths under `/root/.depsilo`,
that is `/root/.depsilo/data/depsilo.db` and `/root/.depsilo/data/cache`, and
copy it into a new state volume before the first start. With relative paths
the container exits with `Permission denied: mkdir /app/data`, because the
service user cannot write the image's working directory `/app`. Put the two
variables into a file that only you can read, here `admin.env`, one
`NAME=value` per line:

```bash
docker volume create depsilo-data
docker run --rm -i \
  --volume depsilo-data:/root/.depsilo \
  --entrypoint sh \
  ghcr.io/depsilo/depsilo:latest \
  -c 'umask 077 && cat > /root/.depsilo/config.toml' < config.toml
docker run -d \
  --name depsilo \
  --env-file admin.env \
  --publish 23333:23333 \
  --volume depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest
```

The log then contains `created initial administrator from environment`.
After signing in, delete `admin.env` and recreate the container without
`--env-file`.

Set both variables. Each one alone also creates the administrator:

- With only `DEPSILO_ADMIN_USERNAME`, Depsilo generates the password. The
  startup summary of that start prints it once under `Initial administrator:`,
  and the same start writes it to the service log. It stays valid until you
  change it, so sign in and change it.
- With only `DEPSILO_ADMIN_PASSWORD`, the user name is `admin`.

With neither variable, a configuration file without an administrator leads to
a reopened setup, which replaces the file; see **Reopened setup** under
[First-run setup](#first-run-setup).

## Administrator access

A local administrator signs in with a user name and a password that Depsilo
stores. Setup creates one such account. Users, API tokens and authentication
settings can be managed only with a local administrator credential, so a
lost password locks you out of them.

**Create a second local administrator.** After setup, open System management → Users,
choose **Add User**, and give the new account the role **Admin**. Keep its
password apart from the first one. An administrator who signs in through OIDC
or LDAP does not replace this account: such a session cannot create or edit
users and cannot change a local password. The rule is stated under
[Sessions, permissions, and revocation](oidc.md#sessions-permissions-and-revocation).

**If the only administrator password is lost.** Use the first of these that
applies:

1. Another local administrator changes it. In System management → Users, edit the account
   and enter a new password. Depsilo keeps running.
2. Reset it on the server with `depsilo reset-password`. Depsilo must be
   stopped for this; the steps follow.
3. [Restore a backup](#back-up-and-restore). Every password returns to its
   value at the time of the backup, and so does the rest of the
   configuration and database.

**Reset a password on the server.**

1. Stop Depsilo. The command refuses to run while a server uses the database.
2. Run the command as the user that runs Depsilo, from the same working
   directory and with the same `--config` or `DEPSILO_CONFIG` as the server:

   ```bash
   depsilo reset-password '<username>'
   ```

   On a terminal it asks for the new password twice, without echo; cancel
   with Ctrl-C. The password follows the rules for `admin.password` under
   [Scripted setup](#scripted-setup). Without a user name, or with one that
   does not exist, the command lists the local accounts and exits with 1.

   Run as root, for example with `sudo`, the command first prints a note
   such as:

   ```text
   Note: the database belongs to uid 10001 and this command runs as root. Lock files it creates are given to the owner of the database directory; lock files that already exist keep their owner.
   ```

   What root leaves behind is described under **Run as root** in
   [Back up and restore](#back-up-and-restore).
3. Start Depsilo and sign in with the new password.

The command changes one thing, the password of an existing local account:

- Every signed-in session of the account becomes invalid.
- API tokens of the account keep working. Revoke them in System management → Users if
  someone else may know them.
- The change is written to the audit log with the action `user.cli_reset` and
  the operating-system user that ran the command.
- A disabled account and an account managed by an external identity provider
  are refused. The command does not create or enable accounts.
- The database is not migrated. Use the Depsilo version that last ran the
  instance; for a container, the image tag of that container.
- The password is never taken from a flag, an argument or an environment
  variable. When standard input is not a terminal, the command reads one
  line from it; redirect it from a file that only you can read.

In Docker, stop the container and run the command in a one-off container that
uses the same volume. Replace `X.Y.Z` with the tag the container runs:

```bash
docker stop depsilo
docker run --rm -it --volumes-from depsilo \
  ghcr.io/depsilo/depsilo:X.Y.Z reset-password '<username>'
docker start depsilo
```

`docker run` does not copy the stopped container's environment. Pass the same
`--env`, `--env-file` and `--user` options as the service container was
started with. In Docker Compose the service's environment is applied
automatically:

```bash
docker compose stop depsilo
docker compose run --rm depsilo reset-password '<username>'
docker compose start depsilo
```

To read the password from a file instead of typing it, replace `-it` with
`-i` for Docker, or add `-T` after `--rm` for Compose, and redirect standard
input:

```bash
docker run --rm -i --volumes-from depsilo \
  ghcr.io/depsilo/depsilo:X.Y.Z reset-password '<username>' < new-password.txt
```

**Guard read-write API tokens.** A read-write API token that belongs to a
local administrator can manage users as that administrator can. This includes
setting a new password for any local account, with
`PUT /api/v1/admin/users/<id>`. Whoever holds such a token can take over the
instance. Create few of them, give each an expiry, store them like the
administrator password, and revoke the ones that are no longer used. A
read-only token cannot change accounts.

## Client setup

The Portal provides package-manager instructions; authenticated operators can
reopen **Connect a client** from Overview. Configuration is generated from
the browser's current origin, including the hostname, HTTPS, and custom port.
Open Depsilo through the address that your client can reach. The Portal does
not change configuration on your machine.

The generated commands therefore contain the address in your browser. Opened
at `http://127.0.0.1:23333`, they work only on the server itself. For clients
on other machines, open Depsilo at the server's network address and copy the
commands from there; a binary installation must first listen on that address
(see [Binary installation](#binary-installation)). When the page is opened on
a loopback address such as `127.0.0.1` or `localhost`, the Portal's Quick
Start and **Connect a client** show a hint that says so.

The authenticated guide observes related requests and distinguishes artifact
delivery from cache hits and policy blocks. Repeat the test following its
instructions to check a cache hit: client caching, server cache rules, and
expiry can affect the result. You can return to Overview at any time;
existing configured deployments do not repeat first-run setup on login.

**Go checksum database.** Keep `GOSUMDB` at its default. For a module that is
not yet in the project's `go.sum`, the `go` command asks its proxy whether it
relays the checksum database. Depsilo passes that question and the checksum
requests that follow, all under `/go/sumdb/`, to its Go Upstreams. It never
contacts `sum.golang.org` itself. It does not check the answers either: the
`go` command verifies them with the database's public key. When a Go Upstream
mirrors the database, checksum requests therefore go through Depsilo, and the
client needs no access to `sum.golang.org`. The `go` command still contacts
the database directly in two cases: no Go Upstream mirrors it, or no Go
Upstream can be reached and Depsilo holds no earlier answer. Depsilo answers
`404` in both, and the client machine must then reach the database. A Go
Upstream mirrors the database when
`<upstream-url>/sumdb/sum.golang.org/supported` answers `200`. Of the two Go
Upstreams that the setup page proposes, `goproxy.cn` answers `200` and
`proxy.golang.org` answers `404`. Where neither a mirroring Upstream nor
`sum.golang.org` is reachable, set `GOSUMDB=sum.golang.google.cn` on the
client.

**Helm chart URLs.** A repository's `index.yaml` usually lists each chart as
an absolute URL. Depsilo serves the index with every chart URL under the
directory of the Upstream that served it made relative: from the Upstream
`https://mirror.azure.cn/kubernetes/charts`, the chart URL
`https://mirror.azure.cn/kubernetes/charts/nginx-1.2.3.tgz` becomes
`nginx-1.2.3.tgz`. Helm resolves it against the repository URL given to
`helm repo add`, so `helm pull` and `helm install` download the chart through
Depsilo, on a project route too. Any other chart URL keeps its value, and
Helm downloads that chart from its origin directly: one that is not a plain
`http`/`https` URL on the Upstream's own scheme, host and directory (such as
another host, `http` under an `https` Upstream, or a URL with a query,
fragment or user information), one written as an escaped quoted string or in a
flow list spanning several lines, a line over 64 KiB, and every URL of an index
in JSON form. An index cached by an earlier version keeps its absolute URLs
until it is next refreshed from the Upstream (`[cache] ttl_index`). A Helm
pool may join different repositories, as the default one does; the cached
index is then the one the member that last refreshed it serves, and a chart
download that one member answers with 404 is asked of the other members, so a
chart listed by an index filled during a failover still downloads after the
first member recovers. When two such repositories hold a chart archive of the
same file name, the first member that has it serves it.

**NuGet resources.** Depsilo writes its own address into the service index for
every resource whose URL is under the URL of the Upstream that served the
index, path included, so a custom feed (Nexus, BaGet, a mirror) is routed
like `api.nuget.org`. A resource on `https://api.nuget.org` is routed the
same way, by its path, when the Upstream is another host: such an Upstream is
taken to serve nuget.org's index unchanged, as a reverse proxy or caching
gateway in front of `api.nuget.org` does, and to serve those resources at the
same paths. Other resources keep their URL. On nuget.org that
includes search and autocomplete, which live on `azuresearch-*.nuget.org`
and not on `api.nuget.org`: `dotnet package search` and IDE search reach them
directly, while restore goes through Depsilo. A feed that names its resources
outside its own URL, as Azure Artifacts does with the feed ID in the path, is
not routed through Depsilo for those resources.

### Optional ecosystems

Docker Registry and Hugging Face have client instructions but are not activated
by the initial setup wizard:

- **Docker:** an administrator can select Docker in **Connect a client** and
  initialize Docker Hub when no registries are configured, the configuration
  file is writable, and no Docker environment override is active. Restart the
  service after saving. Existing custom registries remain file-managed under
  `[docker]` and `[[docker.registries]]`; ordinary Upstream management does not
  change them. The OCI route caches client `GET` and `HEAD` pulls, not image
  pushes or hosted repositories.
- **Hugging Face:** add `[[huggingface.upstreams]]` to the active configuration
  and restart to activate the ecosystem for the first time. After activation,
  its ordinary Upstreams can be added, edited, and removed through Admin's
  Upstreams page without restarting.

Use [`config.example.toml`](../config.example.toml) for the relevant sections and
[Admin control plane](admin-control-plane.md#authority) for configuration versus
runtime ownership. Restart through the process or container that owns your
deployment; saving these initial settings does not restart it automatically.

### AI coding agents

Run this in the project whose dependency commands should use Depsilo:

```bash
depsilo init-agent
```

The command updates a marker-owned section in the detected `AGENTS.md`,
`CLAUDE.md`, or `.cursorrules`, preserving surrounding content. If none is
detected, it creates `AGENTS.md`. Use `--dry-run` to preview changes and
`--endpoint https://depsilo.example.com` to select another instance; without
it the command uses `DEPSILO_URL`, or `http://localhost:23333`.

The section holds the agent prompt of that instance. The prompt lists only
the ecosystems the instance serves. It tells the agent to prefer settings
that live in the project or apply to a single command, and to show the exact
changes and wait for approval before it writes user-level or system-level
configuration. To remove the section again, see [Uninstall](#uninstall).

The command exits with 1 in two cases:

- A target file could not be read or written. Other files may still have
  been updated.
- A file contains the start marker without the end marker. That file is left
  untouched; restore the end marker or remove the block, then run the command
  again.

When the instance cannot be reached, the command writes the prompt built into
the binary, which lists every ecosystem, prints a warning and exits with 0.
With `--json`, the field `"source"` is `"server"` or `"embedded"` accordingly.

**MCP.** The instance serves the Model Context Protocol at `/mcp`, as
JSON-RPC over HTTP `POST`. Every request needs a credential, sent as
`Authorization: Bearer <token>`; without one, every method answers `401`.
With one, `GET` and `DELETE` answer `405`. Use an API token. A read-only
token is enough, because the tools only read. The bootstrap token is only
for first-run setup and is not a package or MCP credential.

A local administrator creates the token in System management → Users, under **API
Tokens**, with **Generate Token**. Depsilo shows the token once. To create it
without a browser, sign in and send one more request. Write the credentials
to `login.json` as `{"username":"<admin-username>","password":"<admin-password>"}`,
in a file that only you can read:

```bash
curl -sS -H 'Content-Type: application/json' \
  --data-binary @login.json \
  https://depsilo.example.com/api/v1/auth/login
```

The answer contains a session token in `"token"`. Put it into a header file,
`session.header`, as the line `Authorization: Bearer <session-token>`, and
create the API token:

```bash
curl -sS -H 'Content-Type: application/json' \
  --header @session.header \
  --data '{"name":"mcp","permissions":"readonly","ttl_days":30}' \
  https://depsilo.example.com/api/v1/admin/tokens
```

`permissions` is `readonly` or `readwrite`; any other value gets `400`.
`ttl_days` is the lifetime in days; leave it out for a token that does not
expire. The answer is `201`, and its `"token"` field, which starts with
`rc_`, is the API token. Delete `login.json` and `session.header` afterwards.
A session that was signed in through OIDC or LDAP cannot create API tokens.

Register the endpoint in the MCP client. For Claude Code, keep the token in
an environment variable, here `DEPSILO_MCP_TOKEN`, and name the variable in
the header. The single quotes keep the shell from replacing it:

```bash
claude mcp add --transport http depsilo https://depsilo.example.com/mcp \
  --header 'Authorization: Bearer ${DEPSILO_MCP_TOKEN}'
```

Claude Code stores the header as written and replaces the variable from its
own environment each time it connects. The token is then neither on the
command line nor in a file. With the variable set,
`claude mcp get depsilo` reports `Status: ✔ Connected`; without it, the
status is `✘ Failed to connect` with HTTP 401.

Without `--scope`, the registration is private to you in the current
project. With `--scope project` the command writes it to `.mcp.json` in the
current directory instead, for everyone who works in the project:

```json
{
  "mcpServers": {
    "depsilo": {
      "type": "http",
      "url": "https://depsilo.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${DEPSILO_MCP_TOKEN}"
      }
    }
  }
}
```

This file holds no token, so it can be committed. Each user sets the
variable to a token of their own. A project-scope registration shows
`Status: ⏸ Pending approval` until that user approves it in `claude`. Do not
commit a `.mcp.json` that contains a real token. Other MCP clients need the
same three values: the HTTP transport, the URL ending in `/mcp`, and the
`Authorization` header.

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

After setup, the browser reconnects through its current scheme, hostname, and
port. For example, an HTTPS reverse proxy can keep serving port 443 while
Depsilo listens on HTTP port 23333 behind it. If you change Depsilo's listener
port, update the proxy target or container port mapping as needed. With direct
access to a changed listener, reopen the new address manually.

For Docker and Compose, persist `/root/.depsilo` with one named volume. This
keeps the generated configuration, administrator and user records, policy
state, SQLite database, and local caches across restart and container
recreation.

The official image runs Depsilo as the fixed non-root UID/GID `10001:10001`.
New named volumes inherit that ownership automatically.

### Bind-mounted state

Use a bind mount when host-side tools must read or back up the state root
directly. Name the host directory `state/` (rather than `data/`) because it
contains `config.toml` as well as the nested `data/` directory. The bind source
must be owned by the image's fixed service identity, `10001:10001`:

```bash
state_dir="${DEPSILO_STATE_DIR:-$HOME/.local/share/depsilo/state}"
mkdir -p "$state_dir"
docker run --rm --user 0:0 \
  --entrypoint /bin/chown \
  --mount "type=bind,src=$state_dir,dst=/root/.depsilo" \
  ghcr.io/depsilo/depsilo:latest \
  -R 10001:10001 /root/.depsilo
```

Start the bind deployment with the checked-in template:

```bash
export DEPSILO_STATE_DIR="$HOME/.local/share/depsilo/state"
export DEPSILO_IMAGE=ghcr.io/depsilo/depsilo:X.Y.Z
docker compose -f compose.bind.yaml up -d
```

The same bind layout can be started directly with Docker Engine:

```bash
docker run -d --name depsilo \
  --restart unless-stopped \
  --publish "${PORT:-23333}:23333" \
  --mount "type=bind,src=$state_dir,dst=/root/.depsilo" \
  "${DEPSILO_IMAGE:-ghcr.io/depsilo/depsilo:X.Y.Z}"
```

Use `--mount type=volume,src=depsilo-data,dst=/root/.depsilo` in the same
command for the named-volume variant. Keep one container and one state source
at a time; changing the source creates a separate state unless you migrate it
explicitly.

The template uses `create_host_path: false`, so a typo or missing directory
fails before the container starts instead of silently creating a root-owned
directory. Do not use `user: root` as a permissions workaround; it can create
state that the fixed service identity cannot reopen after an upgrade.

On SELinux hosts, apply the host's normal container labeling policy to the bind
directory. On rootless Docker, prefer the named-volume template because the
daemon's UID mapping can differ from the numeric UID shown inside the image.

### Other older named-volume layouts

For an older deployment that actually mounted a named volume at
`/root/.depsilo`, resolve the volume from the old container, validate it, stop
that container, and migrate ownership once. Docker and Compose use different
naming conventions and `docker run -v` silently creates a new empty volume
when the name is wrong. The container path remains unchanged, so absolute paths
in the existing configuration remain valid:

```bash
old_container="${OLD_DEPSILO_CONTAINER:-depsilo}"
state_volume="$(docker inspect "$old_container" --format \
  '{{range .Mounts}}{{if and (eq .Type "volume") (eq .Destination "/root/.depsilo")}}{{.Name}}{{end}}{{end}}')"
test -n "$state_volume"
docker volume inspect "$state_volume" >/dev/null
docker stop "$old_container"
docker run --rm --user 0:0 --entrypoint /bin/chown \
  --mount "type=volume,src=$state_volume,dst=/root/.depsilo" \
  ghcr.io/depsilo/depsilo:0.9.1 \
  -R 10001:10001 /root/.depsilo
```

An empty `state_volume` means this is not that layout; stop and inspect the
deployment instead of continuing. Do not use this generic procedure for the
v0.9.0 Compose bind layout described above.

## Overrides

Advanced deployments can move each component independently:

- `DEPSILO_CONFIG` selects a custom configuration file.
- `DEPSILO_SERVER_HOST` and `DEPSILO_SERVER_PORT` select the listen address;
  an IPv6 host may be written with or without square brackets.
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

See [`config.example.toml`](../config.example.toml) for the complete schema and
[Admin control plane](admin-control-plane.md) for runtime ownership and restart
semantics.

## Reverse proxy and TLS

Depsilo serves plain HTTP. For HTTPS, put a reverse proxy in front of it.
Keep Depsilo's own port away from other machines, so that clients cannot go
around the proxy: leave the binary on `127.0.0.1`, or publish the container
port on the Docker host's loopback address only, with
`--publish 127.0.0.1:23333:23333` or `DEPSILO_BIND_ADDRESS=127.0.0.1`.

Depsilo writes its own address into npm, PyPI, Cargo, Composer and NuGet
metadata, into `/api/v1/discover` and into the agent prompts. It takes the
host and port from the request's `Host` header and the scheme from
`X-Forwarded-Proto`. The proxy must send both. Of `X-Forwarded-Proto`,
Depsilo reads the first comma-separated entry, in any letter case; a value
other than `http` or `https` counts as `http`. Depsilo does not read
`X-Forwarded-Host` or `Forwarded`.

For nginx, this server block does it. It works with nginx 1.26:

```nginx
server {
    listen 443 ssl;
    server_name depsilo.example.com;

    ssl_certificate     /etc/nginx/tls/depsilo.example.com.crt;
    ssl_certificate_key /etc/nginx/tls/depsilo.example.com.key;

    proxy_set_header Host              $http_host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;

    proxy_read_timeout   300s;
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:23333;
    }
}
```

| Line | Without it |
| --- | --- |
| `proxy_set_header Host $http_host;` | Metadata sends clients to `http://<backend address>:23333`, and installs fail. Directory (LDAP) sign-in is refused with `403` `ACCESS_DENIED`. |
| `proxy_set_header X-Forwarded-Proto $scheme;` | Addresses in metadata start with `http://`, and the download that follows the metadata fails in npm and pip. |
| `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` | Once `trusted_proxies` lists the proxy, Depsilo believes an `X-Forwarded-For` header that the client sent itself. `X-Real-IP` alone does not prevent that. |
| `proxy_read_timeout 300s;` | nginx answers `504` after 60 seconds, while Depsilo is still moving on to the next Upstream; Depsilo waits up to 60 seconds for each one. |
| `client_max_body_size 0;` | nginx rejects request bodies above 1 MiB with `413`: compiler-cache entries, and snapshot, advisory and distribution imports. Depsilo applies its own limit on each of these routes. |

The event stream, `/api/v1/events/stream`, needs no line of its own. Depsilo
sends it with `X-Accel-Buffering: no`, which turns nginx's buffering off for
that response, and writes a `: keep-alive` line every 25 seconds, so a quiet
stream stays open within nginx's read timeout.

Three more rules for the block:

- Use `$http_host`, not `$host`. `$host` drops the port, which breaks every
  address when the proxy listens on a port other than 443.
- Keep the three `proxy_set_header` lines at `server` level. A `location`
  that sets a header of its own no longer inherits them and must repeat all
  three.
- Do not set `proxy_request_buffering off`. When Depsilo rejects an upload
  early, for example for a wrong token, nginx then answers `502` instead of
  passing Depsilo's answer on.

nginx stores each upload, and the part of a large download that a slow client
has not taken yet, in temporary files. Give the proxy disk space for the
largest upload you allow; a compiler-cache entry can be up to 512 MiB by
default. With `client_max_body_size 0;` at `server` level, nginx accepts a
body of any size on every route before Depsilo can refuse it. On a proxy that
untrusted networks can reach, leave that line out and lift the limit only
where Depsilo takes uploads:

```nginx
    location ~ ^/(ccache|sccache)/ {
        proxy_pass http://127.0.0.1:23333;
        client_max_body_size 0;
    }

    location /api/v1/admin/ {
        proxy_pass http://127.0.0.1:23333;
        client_max_body_size 0;
    }
```

Another proxy needs the same things: the original `Host` with its port,
`X-Forwarded-Proto`, a response timeout above 60 seconds, and no small limit
on request bodies. `X-Accel-Buffering` is an nginx header: on another proxy,
turn off response buffering for the event stream yourself.

**Depsilo settings that go with the proxy.** Addresses in metadata need no
Depsilo setting. Two other things do:

- **Client addresses.** Without `trusted_proxies`, Depsilo sees every request
  as coming from the proxy. The sign-in limiter allows five attempts a
  minute for each client address and user name, so five failed sign-ins for
  a user name then lock that name for everyone behind the proxy, for one
  minute. List the proxy under `[server]` in `config.toml`, or in
  `DEPSILO_SERVER_TRUSTED_PROXIES` as a comma-separated list, and restart:

  ```toml
  [server]
  trusted_proxies = ["127.0.0.1/32"]
  ```

  Entries are IP addresses or CIDR ranges; a host name stops Depsilo at
  startup with `configure server.trusted_proxies: invalid IP address`. The
  address to list is the one Depsilo sees. To find it, make one request
  through the proxy while the list is empty and read the `client_ip` field
  of that request in Depsilo's log. For a container reached through a
  published port, it is the gateway address of the Docker network, not
  `127.0.0.1`. List only proxies that you operate: a listed proxy decides
  the client address of its requests.
- **Compiler cache.** The addresses in a compiler-cache credential come from
  `public_url` under `[compile_cache]`, not from the request. Set it to the
  external origin, for example `https://depsilo.example.com`.

**Check the proxy.** Ask for the service description through the proxy and
pick out `agent_setup.portal_url`:

```bash
curl -s https://depsilo.example.com/api/v1/discover | grep -o '"portal_url":"[^"]*"'
```

The command must print `"portal_url":"https://depsilo.example.com/"`. An
address that starts with `http://` means the `X-Forwarded-Proto` line is
missing. The backend's address or port `23333` means the `Host` line is
missing. Clients that do not trust the certificate's authority need it
named, for example with `--cafile` for npm and `--cert` for pip.

## Upstream network egress

Depsilo connects to package Upstreams and cascade parents only after checking
the addresses their host names resolve to. It never dials link-local,
multicast, documentation, benchmarking or other special-purpose addresses,
except the fake-IP answers of proxy software described next, and a public
Upstream cannot redirect into a host whose answer is a private or loopback
address. Two common networks resolve to such addresses; Admin → Upstreams
names the case on the failing Upstream and in its check result.

**Proxy software in fake-IP mode.** Clash, mihomo, Surge and sing-box in
TUN/fake-IP mode answer DNS queries for public hosts with fake-IP addresses
from `198.18.0.0/15`, and with IPv6 fake-IP on, from an IPv6 range as well.
Their TUN captures every connection to such an address, maps the address back
to its host name and applies the software's rules, as it does for every other
program on the machine. Depsilo connects to such answers the same way: a host
name whose DNS answer consists only of fake-IP addresses from `198.18.0.0/15`
or `2001:2::/48` counts as a public host, and Depsilo dials those addresses,
IPv4 ones first and a quarter second apart, for the TUN to carry. This
applies wherever Depsilo dials itself: package Upstreams, extra indexes and
cascade parents without a proxy, their checks, and Docker registries that no
proxy carries. An address from these ranges written in a URL is still never
dialed, an answer that mixes fake-IP addresses with any other address is
refused, and so is an answer that holds an address of one of this machine's
network interfaces, such as the TUN's own address (`198.18.0.1` for mihomo):
an Upstream check then records `egress_reserved`, and a Docker pull fails with
`unsafe Docker dial target`.

Depsilo's checks cover only the fake-IP answer: a public Upstream or
registry may still not reach a host whose answer is a private or loopback
address, but where a fake-IP name connects is the proxy software's decision.
It resolves the name with its own DNS and follows its rules, and a rule that
connects directly reaches whatever the name's real records name, also
loopback and private addresses of the proxy software's host and network,
for example when a hostile Bearer realm or redirect names such a host. Treat the proxy software's DNS policy and rules as part of the
security boundary, as for a configured proxy: reject loopback, link-local,
metadata-service and unintended private destinations there, or keep such
names on a configured proxy with `allow_fake_ip = false`. The refusal of
this machine's interface addresses does not cover the TUN address of
another host whose TUN carries Depsilo's connections, such as the Docker
host of a Depsilo container or a gateway that runs the proxy software: a
name whose answer is that address, for example one that `fake-ip-filter`
passes with its real record, reaches that host's services that listen on
all addresses or on the TUN address. Fake-IP pools do not hand out the TUN
address. Where such services matter, set `allow_fake_ip = false` in that
setup as well.

The TUN completes a connection to a fake-IP address at once. When none of an
answer's fake-IP addresses connects within 5 seconds each, about 5 seconds in
all, nothing carries them, for example because only the system DNS points at
the proxy software while its TUN mode is off. Upstream checks then fail with
`egress_fake_ip`, and Docker pulls with a `502` that says that nothing
carried the connection (see **Finding the reason**). Turn the proxy
software's TUN mode on, route Upstreams and Docker registries through the
proxy software's HTTP port instead, or give Depsilo a DNS server that
returns real addresses. A registry or Upstream that has not connected
through its fake-IP addresses since Depsilo started has pinned no network
scope, so a real DNS answer, a private one for a host on your network
included, takes effect at the next request without a restart.

`allow_fake_ip = false` under `[egress]` turns the connections to fake-IP
answers off. Depsilo then refuses such an answer before it dials anything, as
releases before this setting did: an Upstream check records `egress_fake_ip`,
and a Docker pull fails with a `502` that names the setting, unless a proxy
carries the request. Turn it off on a network that really uses
`198.18.0.0/15` or `2001:2::/48`, which Depsilo would otherwise dial for any
host name that resolves there; with it off, only a proxy carries fake-IP
names, because the proxy resolves them itself.

```toml
[egress]
upstream_proxy = "http://127.0.0.1:7890"   # the proxy software's HTTP or mixed port
# Optional: hosts that stay direct. An IP address or CIDR range matches only
# a URL written with an address, such as http://192.168.1.20:5000.
no_proxy = ["mirror.lan", "192.168.0.0/16"]
# Optional: refuse fake-IP answers wherever no proxy carries the request.
# allow_fake_ip = false
```

`upstream_proxy` is the default HTTP(S) proxy for every package Upstream and
extra index that has neither its own proxy nor a cascade parent, and for every
Docker registry without its own `proxy`; whether it applies is decided once
per Upstream or registry from the host in its configured URL. An own proxy
always wins, and a URL that names `localhost` or a loopback address is never
proxied. `no_proxy` accepts host names, domains (a leading `.` matches
subdomains only), IP addresses and CIDR ranges. An IP address or CIDR range
matches only a URL written with an address, so list an Upstream or registry
configured by name under that name, even when it resolves into a listed
range. Like an Upstream's own proxy, the proxy then resolves and connects to
every host that Upstream uses, so its egress rules become part of the
boundary, and a plain-HTTP Upstream sends its requests, credentials included,
through the proxy unencrypted. The cascade relay and vulnerability or
blocklist sync keep their own settings.

Depsilo never resolves the host names of a package Upstream that a proxy
carries; the proxy does. `upstream_proxy` therefore also carries an Upstream
on your own network, such as a mirror on the LAN, unless `no_proxy` lists it.
List an Upstream that must stay direct in `no_proxy` by the host in its URL
and restart; Depsilo then connects to it directly. `no_proxy` does not apply
to an Upstream with its own proxy: clear that proxy, and, unless the Upstream
is a cascade, also list its host in `no_proxy` when `upstream_proxy` is set,
or the default proxy carries it instead. The check of a
proxied Upstream reads a failure at the proxy as follows (see
**Finding the reason**):

- `egress_proxy` when Depsilo cannot connect to the proxy, or when the proxy
  answers `CONNECT` for an `https` Upstream with a status other than `200`,
  such as `407` when it asks for credentials, `403` when its rules deny the
  host, or a `5xx` when it cannot reach the host. Admin shows that status.
- `connection_refused` or `network` when the proxy closes the connection
  instead. Proxy software such as Clash, mihomo and sing-box answers
  `CONNECT` with `200` before it connects to the host, and closes the
  connection when it cannot reach it, so an `https` Upstream that such
  software cannot reach fails this way.
- `http_status` for a plain-`http` Upstream, which needs no `CONNECT`, when
  the proxy answers with a `5xx`, such as the `502` that Clash and mihomo
  answer for a host they cannot reach. A `4xx` answer from the proxy passes
  the check, as the Upstream's own would.
- `timeout` when the proxy does not answer in time.

A Docker registry that `upstream_proxy` applies to is routed per request
instead. Depsilo resolves the host of every request itself: the registry's,
its Bearer realm's and each redirect target's. A host whose answer is private
or loopback (RFC 1918, unique-local IPv6 outside the proxy-software ranges
under **IPv6 fake-IP** below, or loopback, whether written as an address or
resolved from a name) is dialed directly, and a host whose answer is public
or consists of fake-IP addresses goes through the proxy. A private
registry whose name resolves to a private address therefore needs no
`no_proxy` entry, while its redirects to public storage keep the proxy. The
answer for the registry's own host pins its network scope until Depsilo
restarts: a registry with a public answer reaches no host whose answer is
private or loopback on either path. A plain-HTTP registry sends what goes through the proxy,
credentials included, unencrypted. Before either path is taken, Depsilo
checks the host; see
[Docker Registry forward-proxy boundary](#docker-registry-forward-proxy-boundary).

Behind proxy software in fake-IP mode, a private registry's name gets a
fake-IP answer unless the software exempts it, and the registry then goes
through the proxy like a public host, which fails when the proxy cannot reach
it. Exempt the names of the registry and of the hosts it uses, such as its
realm and storage, for example with `fake-ip-filter` in Clash and mihomo, so
that they get their real private answers; no `no_proxy` entry is needed.
Restart Depsilo after the change, because a registry keeps the network scope
of its first answer.

Set a registry's own `proxy` when only a proxy reaches the registry, or to keep
all of its traffic on a proxy, for example so that the proxy's access rules
apply to its private hosts too. List a registry in `no_proxy` to keep all of
its traffic off the proxy. A registry kept direct dials the fake-IP answers
for its name and for the hosts it redirects to itself, as a registry without
any proxy does, for the proxy software's TUN to carry. Where nothing carries
them, its pulls fail with the fake-IP `502` described under
**Finding the reason**, and `upstream_proxy` does not help a registry that
`no_proxy` keeps direct: turn the TUN mode on, or give the registry's host
names real DNS answers.

Inside a container, Depsilo still receives fake-IP answers when the Docker
host's proxy software answers its DNS queries, and the host's TUN carries
the connections to them where they pass through it, as they do with
OrbStack. The host's TUN address is then no address of the container's
network interfaces, so Depsilo does not refuse an answer that names it, as
described above for other hosts' TUN addresses. Where the connections do not
pass through the TUN, set `upstream_proxy` to the proxy software on the host. `127.0.0.1` is
the container itself, not the Docker host on which the proxy software runs:
with an `upstream_proxy` on `127.0.0.1`, every Upstream fails with
`egress_proxy`, and Docker pulls fail with `502` and
`connection to the egress proxy failed`, while the server log shows
`proxyconnect tcp: dial tcp 127.0.0.1:7890: connect: connection refused`.
Give the host's address as the container sees it, for example
`http://host.docker.internal:7890`. Docker Desktop and OrbStack
resolve that name without further options. On Docker Engine for Linux the
name exists only when you add it: Docker documents the special value
`host-gateway`, which resolves to the internal IP address of the host, for
`--add-host=host.docker.internal:host-gateway` on `docker run` and for
`extra_hosts` in Compose. The option does no harm where the name already
resolves, so the examples under **Environment variables** below include it.
On Docker Engine for Linux, `host.docker.internal` then stands for an
address of the host other than `127.0.0.1`. Proxy software that listens only
on the host's `127.0.0.1` does not accept connections to it. Let the proxy
software listen on an address that containers can reach; Clash and mihomo
call that option `allow-lan`.

**IPv6 fake-IP.** With IPv6 fake-IP on, proxy software also answers AAAA
queries with fake-IP addresses. Depsilo treats `2001:2::/48`, the IPv6
benchmarking range, like `198.18.0.0/15`, and where it dials fake-IP answers
itself, it dials an answer's addresses from this range after its IPv4 ones,
because a TUN carries IPv6 only when IPv6 is on for it. It cannot tell a
unique-local fake-IP address from a private network, and treating one as
private would let a public registry's Bearer realm or redirect reach private
addresses. The unique-local ranges that proxy software documents for IPv6
fake-IP and for its own TUN, VIF and DNS addresses, `fdfe:dcba:9876::/48`,
`fd00:6152::/32` and `fc00::/18`, are therefore refused for every Upstream,
cascade parent and Docker registry and never count as a private network, and
an answer that holds one is not dialed even where the proxy software's TUN
would carry it; a failed Upstream check records `egress_fake_ip`. Package
Upstreams behind `upstream_proxy` are not affected, because the proxy
resolves their host names. Docker registries are affected even through a
proxy, because Depsilo checks the answers for their
host names before the proxy carries a request: a registry, Bearer realm or
redirect host name whose answer consists of fake-IP and unique-local
addresses only, with at least one unique-local address and either a fake-IP
address or an address in one of those ranges, fails with a `502` that asks
for the setting below (see **Finding the reason**). A unique-local answer
outside those ranges and without fake-IP addresses stays a private network.
Set the IPv6 fake-IP range to `2001:2::/48`, or a part of it, or turn IPv6
fake-IP off:

| Proxy software | Setting |
| --- | --- |
| mihomo | `fake-ip-range6: 2001:2::/64` under `dns`, or IPv6 fake-IP off with `ipv6: false` under `dns` |
| Clash Verge Rev | `fake-ip-range6: 2001:2::/64` in its DNS settings, or its IPv6 switch off. Since 2.5.5 it fills in this range by itself in TUN fake-IP mode when no `fake-ip-range6` is set; its DNS-settings template writes `fdfe:dcba:9876::1/64`. In TUN fake-IP mode it also sets `ipv6` under `dns` from its IPv6 switch, so `ipv6: false` under `dns` has no effect there |
| sing-box | `"inet6_range": "2001:2::/48"` on the `fakeip` DNS server, as its examples use since 2026-10 (they used `fc00::/18` before) |
| v2rayN | the `2001:2::/48` choice for the IPv6 fake-IP range, or IPv6 fake-IP off |
| Surge | `ipv6-vif = disabled`, its default. Surge's IPv6 fake-IP prefix lies under `fd00:6152::` and cannot be changed. It answers A queries only over IPv4 and AAAA queries only over IPv6, so through its IPv6 DNS address a host resolves to `fd00:6152::` addresses alone |

**Overlay networks.** Tailscale, Headscale and NetBird assign node addresses
from the shared address space `100.64.0.0/10`, which Depsilo also refuses by
default. Declare the part your own nodes use:

```toml
[egress]
private_ranges = ["100.64.0.0/10"]   # or narrower ranges, or single addresses
```

Declared addresses are treated as private networks: an Upstream or cascade
parent configured with such an address (or a name resolving to it) is
reachable, and it may reach other private addresses, but a public Upstream
still cannot redirect into them. Only ranges inside `100.64.0.0/10` are
accepted, `100.100.100.200` (the Alibaba Cloud instance-metadata endpoint)
stays blocked, and `198.18.0.0/15` is rejected with a pointer to
`allow_fake_ip` and `upstream_proxy`: fake-IP answers need no declaration,
and treating them as private would let every public Upstream redirect into
the local network. Docker registries do not use
`private_ranges`: a registry, Bearer realm or redirect target that resolves
into `100.64.0.0/10` is refused with or without a proxy. `[egress]` settings
are read at startup; restart after changing them.

**Environment variables.** Each `[egress]` setting has an environment variable
that overrides the value in `config.toml`. For a container, this avoids
editing a file inside the state volume:

| Variable | Overrides | Format |
| --- | --- | --- |
| `DEPSILO_EGRESS_UPSTREAM_PROXY` | `upstream_proxy` | One `http://` or `https://` proxy URL |
| `DEPSILO_EGRESS_NO_PROXY` | `no_proxy` | Comma-separated list |
| `DEPSILO_EGRESS_PRIVATE_RANGES` | `private_ranges` | Comma-separated list |
| `DEPSILO_EGRESS_ALLOW_FAKE_IP` | `allow_fake_ip` | `true` or `false`; `true` when neither sets it |

```bash
docker run -d \
  --name depsilo \
  --publish 23333:23333 \
  --volume depsilo-data:/root/.depsilo \
  --add-host=host.docker.internal:host-gateway \
  --env DEPSILO_EGRESS_UPSTREAM_PROXY=http://host.docker.internal:7890 \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:latest
```

For Compose, save this as `compose.override.yaml` beside `compose.yaml` and
run `docker compose up -d`:

```yaml
services:
  depsilo:
    environment:
      DEPSILO_EGRESS_UPSTREAM_PROXY: http://host.docker.internal:7890
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

To change the variable on an existing container, remove the container and run
it again on the same volume; the instance is kept.

A list variable replaces the whole list from `config.toml`; the two are not
merged. Spaces around the entries are ignored, so
`mirror.lan, 192.168.0.0/16` works. An empty variable counts as unset. Values
are checked like those in the file, and an invalid one stops startup; a
trailing comma, for example, leaves an empty entry. The variables are read at
startup, like the file.

**Finding the reason.** A failed Upstream check records a failure code.
Depsilo shows it in four places:

- Admin → Upstreams, on the failing Upstream and in its check result.
- Admin → **Connect a client**, in the warning for the selected ecosystem,
  when the code is `egress_fake_ip`.
- `depsilo doctor`, when it has an Admin token in `DEPSILO_TOKEN` or
  `~/.config/depsilo/token`. Its Upstream health line then ends with the codes
  and their counts, for example `failure codes: egress_fake_ip x2, timeout x1`.
  Without a token, doctor prints a general hint.
- The `502` answer to a package client. When every Upstream of the ecosystem
  failed for the same egress reason, a refusal by the outbound guard, a
  fake-IP answer that nothing carried, or a failure at the egress proxy, the
  message names it, for example:

  ```text
  all upstreams are unhealthy: DNS returned fake-IP addresses for every upstream (egress_fake_ip), and Depsilo could not connect to them: nothing carried the connections, [egress] allow_fake_ip is off, or the answers mixed fake-IP and other addresses or held unique-local IPv6 fake-IP addresses, which Depsilo always refuses; turn on the proxy software's TUN mode, or set [egress] upstream_proxy in config.toml (in a container, the environment variable DEPSILO_EGRESS_UPSTREAM_PROXY) and restart, or check allow_fake_ip; for unique-local IPv6 fake-IP addresses, set the proxy software's IPv6 fake-IP range to 2001:2::/48, see docs/deployment.md#upstream-network-egress
  ```

  or, when every check failed at the proxy:

  ```text
  all upstreams are unhealthy: every upstream failed at the egress proxy (egress_proxy); check that the proxy runs and that [egress] upstream_proxy or an upstream's own proxy points at it; to keep hosts on your own network off upstream_proxy, list them in [egress] no_proxy and restart, see docs/deployment.md#upstream-network-egress
  ```

  In every other case the message is `all upstreams are unhealthy` alone.

| Code | Meaning |
| --- | --- |
| `egress_fake_ip` | DNS answered with fake-IP addresses, as proxy software in fake-IP mode does, and Depsilo could not use them: none of the addresses in `198.18.0.0/15` or `2001:2::/48` that it dialed connected within 5 seconds, so nothing carried the connection; `allow_fake_ip` is off; or the outbound guard always refuses the answer, because it mixes fake-IP and other addresses, holds an address in a unique-local range of proxy software (`fdfe:dcba:9876::/48`, `fd00:6152::/32`, `fc00::/18`), or is an address from these ranges written in the Upstream URL. Admin shows the address that was dialed first or refused, the unique-local one when the answer holds one, and its range |
| `egress_shared_address` | The host resolved into `100.64.0.0/10`, and no `private_ranges` entry covers the address |
| `egress_reserved` | The host resolved to another special-purpose address, the DNS answer mixed network scopes, or a fake-IP answer held an address of this machine's network interfaces, such as the TUN address of the proxy software |
| `egress_private` | The request was directed to a private or loopback address that this Upstream may not reach |
| `egress_proxy` | The request failed at the proxy that carries the Upstream, `upstream_proxy` or its own: Depsilo could not connect to the proxy, or the proxy answered `CONNECT` for an `https` Upstream with a status other than `200`, which Admin shows. Other failures at the proxy read as the codes below; see the list for proxied Upstreams above |
| `dns` | The host name could not be resolved |
| `timeout` | The connection or the response timed out, also when the egress proxy does not answer in time |
| `tls` | The TLS handshake or certificate verification failed |
| `connection_refused` | The connection was refused or reset; through an egress proxy, also a tunnel that the proxy reset |
| `http_status` | The Upstream answered with an HTTP status that marks it unhealthy; for a plain-`http` Upstream, also a `5xx` that its egress proxy answered |
| `network` | Any other network failure; through an egress proxy, also a tunnel that the proxy closed |

A Docker registry has no check, so Admin and doctor show no failure code for
it. A Docker request that Depsilo cannot complete against the registry gets a
`502` whose message names what failed, where, and why, and ends with the
request ID:

```text
<stage> failed at <origin>: <reason> (request <id>; details in the Depsilo server log)
```

The stage is `auth discovery`, `token fetch`, `tag resolution`, `manifest`,
`manifest HEAD`, `blob` or `tag list`. The origin is the scheme, host and port
of the host that failed: the registry, its Bearer realm, or a host that a
redirect named, such as a CDN or object storage. The reason is one of:

- `returned <status>`, followed by at most three error codes of the OCI
  distribution specification that the answer carried, for example
  `returned 429 (TOOMANYREQUESTS)` or `returned 403 (DENIED)`;
- a failure code from the table above: `timeout`, `tls`,
  `connection_refused` or `network`;
- `connection to the egress proxy failed` when the proxy that carries the
  registry cannot be reached, also when its host name does not resolve, or
  `egress proxy refused CONNECT (403)`, with the status the proxy answered,
  when it refuses to connect to an `https` origin;
- `returned <status> through the egress proxy` when `upstream_proxy` carried
  a plain-`http` request and the answer was `502`, `503` or `504`. A proxy
  answers so when it cannot reach a host, and Depsilo cannot tell that answer
  from the registry's own, so a plain-`http` registry's own `502`, `503` or
  `504` through `upstream_proxy` reads the same way, and a manifest `HEAD`
  then answers `502` instead of passing the status on; a cached manifest
  still answers such a `HEAD`;
- one of Depsilo's own refusals, such as `unsafe Docker redirect target` or
  `unsupported authentication challenge`, or the fake-IP and IPv6 fake-IP
  refusals described below. Depsilo resolves the registry, realm and
  redirect hosts itself before it connects, so such a host name that does
  not resolve reads `resolve Docker proxy target` when the registry has a
  proxy, its own or `upstream_proxy`, and `resolve Docker dial target`
  otherwise, rather than the `dns` failure code. On the direct path of a
  registry that `upstream_proxy` applies to, Depsilo resolves the host again
  when it connects: `resolve Docker dial target` then means that this lookup
  failed, and `Docker direct target left the private network` that its
  answer was no longer private or loopback; the server log names that
  answer.

Any other failure reads `Docker registry request failed`. The message never
repeats text that a registry, proxy or other server sent: no answer body,
error message, status phrase or malformed answer, no path or query of a URL
(signed CDN and object-storage addresses carry their credentials there), and
no proxy address.

When no proxy carries a registry (it has no `proxy` of its own, and
`upstream_proxy` is unset or `no_proxy` or a loopback URL keeps it direct),
Depsilo dials the fake-IP addresses of a registry, Bearer realm or redirect
host name whose answer consists only of addresses from `198.18.0.0/15`,
`2001:2::/48` or both. When none of them connects, the reason names the host
and the address dialed first, an IPv4 one when the answer has one, for
example:

```text
auth discovery failed at https://registry-1.docker.io: registry-1.docker.io resolved to 198.18.0.10, a fake-IP address from proxy software, but nothing carried the connection to it; turn on the proxy software's TUN mode, or set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to its HTTP port and restart, see docs/deployment.md#upstream-network-egress (request 4f0c9a52-8d1e-4b7a-9c3f-2e6d5b1a0f87; details in the Depsilo server log)
```

The server log line ends with the errors of the dials, after `dial error:`.
When `no_proxy` or a loopback URL keeps the registry off a configured
`upstream_proxy`, setting the proxy cannot help, so the remedy after
`nothing carried the connection to it;` reads instead
`turn on the proxy software's TUN mode, or, as [egress] no_proxy or a loopback URL keeps this registry off the proxy, give its host names real DNS answers, for example with fake-ip-filter, or remove it from no_proxy, see docs/deployment.md#upstream-network-egress`.

With `allow_fake_ip` off, Depsilo refuses such a host name before it dials
anything, and the reason reads `<host> resolved to <address>, a fake-IP
address from proxy software in TUN/fake-IP mode, and [egress] allow_fake_ip
is off;` followed by
`set [egress] upstream_proxy or DEPSILO_EGRESS_UPSTREAM_PROXY to the proxy software's HTTP port and restart, see docs/deployment.md#upstream-network-egress`,
or, for a registry that `no_proxy` or a loopback URL keeps off a configured
`upstream_proxy`, by
`[egress] no_proxy or a loopback registry URL keeps this registry off the proxy, so give its host names real DNS answers, for example with fake-ip-filter in the proxy software, or remove the registry from no_proxy, see docs/deployment.md#upstream-network-egress`.
A fake-IP answer that holds an address of this machine's network interfaces
reads `unsafe Docker dial target`, or `unsafe Docker proxy target` when a
proxy carries the registry, and the server log names that address.

When the registry, its Bearer realm or a redirect target has a host name
whose answer is unique-local IPv6 fake-IP as described under
**IPv6 fake-IP** above, the reason names the host and asks for the proxy
software's IPv6 fake-IP range instead, whether or not a proxy carries the
registry. It names the documented range that one of the addresses lies in,
for example:

```text
auth discovery failed at https://registry-1.docker.io: registry-1.docker.io resolved into fdfe:dcba:9876::/48, an IPv6 fake-IP range that Depsilo cannot tell from a private network; set the proxy software's IPv6 fake-IP range (fake-ip-range6 or inet6_range) to 2001:2::/48 or turn IPv6 fake-IP off, see docs/deployment.md#upstream-network-egress (request 4f0c9a52-8d1e-4b7a-9c3f-2e6d5b1a0f87; details in the Depsilo server log)
```

When none of the answer's unique-local addresses lies in a documented range,
the reason reads `<host> resolved to fake-IP addresses and a unique-local
address, which Depsilo cannot tell from a private network;` followed by the
same remedy. Such a prefix can be an internal network's, so the message names
no unique-local address or prefix; the server log line lists the whole
answer after `DNS answer:`.

When `upstream_proxy` carries a request that fails before Depsilo gets a
usable answer, the reason can end with a hint that names the host. For a host
whose answer consisted of fake-IP addresses, the hint asks for a real DNS
answer when the proxy refused `CONNECT`, when the connection through the
proxy failed in any other way, for example because the proxy closed it after
accepting it (mihomo does so for an `https` host it cannot reach) or because
it timed out, or when the proxy answered a plain-`http` request with `502`,
`503` or `504`. A TLS failure, whose reason reads `tls`, gets no hint,
because the proxy did reach a server. For example:

```text
auth discovery failed at https://registry.home.lan: egress proxy refused CONNECT (502); if registry.home.lan is on your network, give it a real DNS answer, for example with fake-ip-filter in the proxy software, so that Depsilo connects to it directly after a restart, see docs/deployment.md#upstream-network-egress (request 4f0c9a52-8d1e-4b7a-9c3f-2e6d5b1a0f87; details in the Depsilo server log)
```

The registry's own host needs the restart because its first answer pins the
registry's network scope until Depsilo restarts; a changed answer before
that reads `Docker registry changed network scope`. For a realm or redirect
host, the hint ends with `directly, see` instead. For the registry's own
host with a public answer, when the proxy refused `CONNECT` or answered a
plain-`http` request with `502`, `503` or `504`, the hint reads `; if <host>
is reachable only without the proxy, list it in [egress] no_proxy and
restart, see docs/deployment.md#upstream-network-egress`. No other
host gets that hint, because `no_proxy` decides from the host in a registry's
URL. The fake-IP hint is given for the registry's own host, and for a realm
or redirect host only when the registry's answer was private or loopback: a
registry with a public or fake-IP answer may not reach a host that answers
privately. A proxy that cannot be reached, a registry's own `proxy` and the
direct path give no hint.

The request ID leads to the details. Every answer carries it in the
`X-Request-ID` header. Admin → Access Logs shows it in a request's details,
and the MCP tool `depsilo_recent` explains one request given its
`request_id`, a policy-blocked request included: a block writes no access-log
row, so the tool answers from the audit log and says so in `source`. The
server log line of the failure carries it in the
`request_id` field, next to the full error: the URL that failed and up to
64 KiB of the registry's answer. In that URL, and in every absolute `http` or
`https` URL and every double-quoted URL reference in the error text or the
answer, userinfo is dropped and each query value reads `REDACTED`. Look there
when the client shows no message, for example when containerd resolves a tag
with `HEAD`: the answer to a `HEAD` request has no body. A manifest `HEAD`
appears among the access logs whatever the registry answers, and one that
fails with a `5xx`, whether Depsilo answers `502` or passes on the registry's
own `5xx`, also has its server log line. One exception: with the
observation-age gate armed, a registry `4xx` to tag resolution is passed on
and recorded only in the audit log. At
startup the server log names each registry's route in a
`docker registry egress` line: `own proxy`,
`default proxy (private answers direct)` or `direct`. On the route of
`upstream_proxy`, a `docker registry host dialed directly` line names the
registry and the host, without its address, the first time Depsilo dials
that host directly.

## Docker Registry forward-proxy boundary

Before a forward proxy carries a Docker Registry request (the registry's own
`proxy`, or `[egress] upstream_proxy` for a registry without one), Depsilo
resolves the registry, Bearer realm, or redirect hostname locally. It rejects
unresolved targets, mixed network scopes, unsafe special-purpose addresses, and
a target that crosses the registry's pinned public/private/loopback scope. One
answer is the exception: when every address a host name resolves to lies in
`198.18.0.0/15` or `2001:2::/48`, in either family or both and in any order,
the answer comes from proxy software in fake-IP mode and says nothing about
the real address, so Depsilo treats the host as public and lets the proxy
resolve it. An answer that mixes such addresses with any other address, an
answer that holds an address of this machine's network interfaces, and an
address from either range written in a URL are still rejected. Without a
proxy, the same exception applies while `[egress] allow_fake_ip` is on, the
default: Depsilo dials the fake-IP addresses itself for the proxy software's
TUN to carry (see [Upstream network egress](#upstream-network-egress)), and
with it off, it rejects such an answer. Unique-local IPv6 fake-IP gets no
such exception: an address in a unique-local range of proxy software
(`fdfe:dcba:9876::/48`, `fd00:6152::/32`, `fc00::/18`) is rejected, never
treated as a private network, and so is an answer that puts fake-IP
addresses next to any unique-local address; see
[IPv6 fake-IP](#upstream-network-egress). The proxy socket itself may
intentionally be a local service such as `http://127.0.0.1:7890` and is not
classified as an Upstream target.

For a registry that `[egress] upstream_proxy` applies to, the same check also
routes the request. A host whose answer passed as private or loopback is
dialed directly: Depsilo resolves it again when it connects and dials only
an answer that is still private or loopback and that the registry's pinned
scope allows. An answer that is no longer private or loopback fails with
`Docker direct target left the private network`. Only hosts whose answers
are public or fake-IP reach the proxy. A registry's own `proxy` carries every
request as before.

For a registry configured by host name, a Bearer realm or redirect target
written as a private or loopback IP address, such as a storage redirect to
`http://10.0.0.30:9000`, is checked like a host name: the registry's pinned
scope decides, so a registry whose answer is private follows a private
address, one whose answer is loopback also a loopback address, and one whose
answer is public neither. `localhost`, `.localhost` names and addresses
written with a trailing dot are still refused as such targets, because a
resolver, the proxy's included, may answer them with any address. A registry
configured by address may reach a private or loopback target only when its
own address allows it, and a target written as an unsafe address is always
refused.

This preflight is not an end-to-end DNS pin when the forward proxy performs its
own name resolution. In split-DNS deployments, the proxy can receive a
different address from the one Depsilo checked, and for a host answered with
fake-IP addresses the proxy's resolution is the only one. Treat the proxy's
resolver and egress ACL as part of the security boundary: block loopback,
link-local, metadata-service, and unintended private destinations there as
well. A name whose real DNS records lie in `198.18.0.0/15` or `2001:2::/48`
also counts as public, so the proxy is asked to connect to such an address,
including the TUN address of proxy software on the proxy's host when it lies
in one of these ranges; only an address of Depsilo's own machine is refused.
Without a proxy, Depsilo connects to such an address itself while
`allow_fake_ip` is on: a TUN that carries the connection is then the
resolver whose DNS policy and rules form this boundary, and where no TUN
carries it, the network it is routed to decides what answers. The TUN
address of another host whose TUN carries Depsilo's connections, such as the
Docker host of a Depsilo container, is not refused either. Through
`upstream_proxy`, this concerns only hosts
with public or fake-IP answers: a registry's private and loopback targets are
dialed directly, so the proxy no longer resolves them, and its access rules
no longer apply to them either. A registry whose answer is private may reach
every private address that way, by name or by address. Set the registry's
own `proxy` to keep its private targets behind the proxy's access rules.

Docker authentication services are commonly on a different public origin from
the registry itself; Docker Hub, for example, delegates authentication to
`auth.docker.io`. Depsilo therefore allows a cross-origin Bearer realm when its
resolved network scope is safe, while removing registry credentials from later
cross-origin redirects. Configuring registry credentials means trusting that
registry to nominate its authentication service.

## When Upstreams are unreachable

Expiry never deletes cached content. When a package index expires, Depsilo
refreshes it before answering; if the refresh fails, or is still waiting after
10 seconds, the cached index is served while the refresh continues. Cached
artifacts are served directly, so only content that was never cached needs a
reachable Upstream.

An Upstream whose requests fail slowly twice in a row, each waiting at least
10 seconds as on a link that drops packets, leaves selection. Depsilo re-probes
an active Upstream after 30 seconds and then at doubling delays of up to 5
minutes rather than its regular probe interval; a passive Upstream receives one
half-open request per minute instead. Either rejoins once it answers. Failures
that return at once, such as a refused connection, do not count: the cached
copy already answers them immediately, and the request moves on to the next
Upstream.

While the known-malicious dataset covers PyPI, a legacy archive such as a
`.zip` sdist proves its identity from the cached project index, so it stays
available after a restart without network.

Docker clients that resolve a tag with `HEAD`, such as containerd, receive the
cached manifest's digest while the registry or its token service is
unreachable. With the observation-age gate enabled, a tag still needs the
registry: its current digest cannot be confirmed offline.

## Health checks

`status` reports service health and available statistics; `doctor` performs
read-only checks for reachability, readiness, version mismatch, storage,
Upstream health, and recent cache activity, with actionable hints:

```bash
depsilo --version
depsilo status
depsilo doctor
curl -fsS http://127.0.0.1:23333/ready
```

For Docker, run them inside the container; with Compose, the commands start
with `docker compose exec depsilo depsilo`:

```bash
docker exec depsilo depsilo status
docker exec depsilo depsilo doctor
```

For a different port, a different listen address or a remote instance,
select its origin explicitly:

```bash
DEPSILO_URL=http://127.0.0.1:18080 depsilo doctor
```

The CLI otherwise uses `http://localhost:23333`; `status` and `doctor` do not
derive the target from a custom server configuration file. Both support
`--json`. Some checks need an Admin credential; set `DEPSILO_TOKEN` or use the
CLI's `~/.config/depsilo/token` file to include authorized details. Missing
credentials are reported separately from service failure. The `/ready` probe
checks database and storage availability; it does not prove that every
Upstream is reachable or that a dependency download will succeed.

`doctor` exits with 1 when any check fails. `status` exits with 1 only when
the service cannot be reached or is not healthy.

While first-run setup is pending, the service is healthy, `/ready`
answers `200`, and package routes serve from the built-in Upstreams, but no
administrator can sign in yet. `doctor` then reports an informational
`Administrator` check that points at `/admin` and does not fail because of
it. `status` prints a line that starts with `Admin:   no administrator yet`
and exits with 0. Both `--json` outputs carry
the boolean `setup_required`.

When Upstreams fail and a token is configured, the Upstream health line of
`doctor` ends with the failure codes and their counts. The codes are listed
under [Upstream network egress](#upstream-network-egress). A read-only API
token is enough for this.

### Diagnostic report

To collect a local report for troubleshooting:

```bash
depsilo diagnose --out depsilo-diagnostic.json
```

The report uses the same `DEPSILO_URL` selection, includes enumerated health
and capability facts, and omits configuration, credentials, request samples,
package identities, and client addresses. It is created with owner-only
permissions and refuses to overwrite an existing file; choose a new output
name for each report.

## Back up and restore

`depsilo backup` writes one archive while Depsilo keeps running.
`depsilo restore` puts an archive back and needs Depsilo stopped. Run both as
the user that runs Depsilo, or as root as described under **Run as root**
below, and from the same working directory as the server:

```bash
depsilo backup --out depsilo-backup.tar.gz
depsilo restore depsilo-backup.tar.gz
```

Neither command takes `--config`. If the server is started with
`--config PATH`, set `DEPSILO_CONFIG=PATH` for both commands, for example:

```bash
DEPSILO_CONFIG=/var/lib/depsilo/config.toml depsilo backup --out depsilo-backup.tar.gz
```

Without the variable, both commands use the default paths under
`~/.depsilo`. When no instance is there, `backup` stops with
`load config: instance signing key is missing`, and `restore` writes the
files there and leaves the server's instance as it was.

Without `--out`, the archive is `depsilo-backup.tar.gz` in the current
directory. The directory of the archive must be writable, because the backup
is assembled beside it. A directory in `--out` that does not exist yet is
created with mode `0700`.

**Run as root.** Both commands, and `depsilo reset-password`, may run as root,
for example with `sudo` or as UID 0 in a container. In a directory that
belongs to another account, every file and directory they create there is
given to that directory's owner and group, so the service account can still
use the instance. Entries that already exist keep their owner. A backup
archive written outside the directories of `config.toml` and the database
stays owned by root. A backup killed while it runs leaves a root-owned
`.depsilo-backup-work-*` directory beside the archive; remove it as root.
When the owner cannot be changed, `restore` stops before it replaces
anything and prints an error that ends with:

```text
give new entry "<path>" to uid <uid>, gid <gid>, the owner of directory "<directory>": <cause>
```

An older version run as root, or a root command killed at the wrong moment,
can leave the lock directory `.depsilo-locks` beside the database owned by
root. Depsilo does not repair it, and the service stops at startup with
`Permission denied: chmod <database directory>/.depsilo-locks`. Give it back
to the service account. For the binary, run
`sudo chown -R <service user>:<service group> <database directory>/.depsilo-locks`.
In Docker, stop the container and run `chown` as root in a one-off container
that uses the same volume. Replace `X.Y.Z` with the tag the container runs:

```bash
docker stop depsilo
docker run --rm --user 0:0 --volumes-from depsilo --entrypoint /bin/chown \
  ghcr.io/depsilo/depsilo:X.Y.Z -R 10001:10001 /root/.depsilo/data/.depsilo-locks
docker start depsilo
```

**What the archive contains.**

| File | Content |
| --- | --- |
| `config.toml` | The configuration, with the JWT signing key written into it, also when the instance keeps the key in `data/secrets/jwt.key` |
| `depsilo.db` | A consistent snapshot of the SQLite database, which holds the accounts with their password hashes, the API tokens, policy state, and the access and audit logs |
| `manifest.json` | The size and SHA-256 of the two files |

The archive is created with mode `0600`. It holds the signing key and the
password hashes, so keep it as private as the instance itself.

**What it does not contain.**

- Cached package objects and compiler-cache objects, whether they are in a
  local directory or in S3. Depsilo fetches packages again when they are
  requested; copy the storage directory or bucket yourself to keep them.
- The object bytes of retained dependency sets. The database in the archive
  lists them, but the bytes are stored apart; see
  [Delete, reclaim, and recover](distribution.md#delete-reclaim-and-recover).
- Files that `config.toml` names by path, such as
  `transfer_signing_key_file` under `[distribution]`. Keep a copy of each.
  With distribution enabled, a restored instance does not start while that
  key file is missing; the error starts with
  `distribution.transfer_signing_key_file:`.
- Settings that exist only in the service's environment, such as `DEPSILO_*`
  variables of a container. Keep them with your deployment files.

The format and the versions that `restore` accepts are promised under
[Database and backups](compatibility.md#database-and-backups).

**Restore.**

1. Stop Depsilo. While a server uses the database, the command stops with
   `restore requires the Depsilo server to be stopped`.
2. Run `depsilo restore depsilo-backup.tar.gz`. The archive is checked before
   anything is replaced. The files that were in place stay beside the
   restored ones as `config.toml.pre-restore-<id>.bak` and
   `depsilo.db.pre-restore-<id>.bak`.
3. Start Depsilo. Accounts, passwords, API tokens and settings are those of
   the backup. Remove the two `.bak` files once the instance works; they
   still hold the accounts and settings from before the restore.

When the current configuration cannot be read, name the two files to replace
with `--config-target` and `--database-target`.

**In a container.** Always pass `--out` with a path that the service user can
write, such as one under `/tmp`. Without it the command fails, because the
working directory `/app` is not writable:

```text
Error: cannot write the backup into /app: permission denied. Pass --out with a writable path; in the official container image the state directory is writable, for example: --out /root/.depsilo/backups/depsilo-backup.tar.gz
```

The state directory works too, but the archive then stays in the volume.
Copy the archive out and delete the copy in the container:

```bash
docker exec depsilo depsilo backup --out /tmp/depsilo-backup.tar.gz
docker cp depsilo:/tmp/depsilo-backup.tar.gz ./depsilo-backup.tar.gz
docker exec depsilo rm /tmp/depsilo-backup.tar.gz
```

With Compose:

```bash
docker compose exec -T depsilo depsilo backup --out /tmp/depsilo-backup.tar.gz
docker compose cp depsilo:/tmp/depsilo-backup.tar.gz ./depsilo-backup.tar.gz
docker compose exec -T depsilo rm /tmp/depsilo-backup.tar.gz
```

To restore, stop the container and run the command in a one-off container on
the same volume. The archive is read from standard input, so no host
directory is mounted. Replace `X.Y.Z` with the tag the container runs, and
pass the same `--env`, `--env-file` and `--user` options as the service
container was started with:

```bash
docker stop depsilo
docker run --rm -i --volumes-from depsilo --entrypoint sh \
  ghcr.io/depsilo/depsilo:X.Y.Z \
  -c 'umask 077 && cat > /tmp/depsilo-backup.tar.gz && exec depsilo restore /tmp/depsilo-backup.tar.gz' \
  < depsilo-backup.tar.gz
docker start depsilo
```

With Compose:

```bash
docker compose stop depsilo
docker compose run --rm -T --entrypoint sh depsilo \
  -c 'umask 077 && cat > /tmp/depsilo-backup.tar.gz && exec depsilo restore /tmp/depsilo-backup.tar.gz' \
  < depsilo-backup.tar.gz
docker compose start depsilo
```

To move the instance to a new, empty volume, replace
`--volumes-from depsilo` in the `docker run` command with
`--volume <new-volume>:/root/.depsilo`, then start a container on that
volume.

## Upgrade

1. [Back up](#back-up-and-restore) the instance and copy the archive off the
   host. The backup is the only way back to the older version.
2. Replace the program, as described for your installation below.
3. Check the result:

   ```bash
   curl -fsS http://127.0.0.1:23333/ready
   depsilo doctor
   ```

   For a container, run `docker exec depsilo depsilo doctor`. The
   `Version match` line of `doctor` names the version that is running.
   `doctor` exits with 1 when any check fails; see
   [Health checks](#health-checks).

**Docker.** Pull the release tag you want, then remove the container and run
it again on the same volume, with the options it had before:

```bash
docker pull ghcr.io/depsilo/depsilo:X.Y.Z
docker stop depsilo
docker rm depsilo
docker run -d \
  --name depsilo \
  --publish 23333:23333 \
  --volume depsilo-data:/root/.depsilo \
  --restart unless-stopped \
  ghcr.io/depsilo/depsilo:X.Y.Z
```

**Docker Compose.** Set `DEPSILO_IMAGE=ghcr.io/depsilo/depsilo:X.Y.Z` in the
deployment's `.env` file, then pull the image and recreate the container:

```bash
docker compose pull
docker compose up -d
```

Do not run `docker compose down -v`: `-v` deletes the state volume.

**Binary.** Stop Depsilo, install the release over the old binary, and start
it again. Pass `DEPSILO_INSTALL_DIR` as well when the binary is not in
`/usr/local/bin`; see [Installer options](#installer-options):

```bash
depsilo stop
curl -fsSL https://depsilo.com/install.sh | DEPSILO_VERSION=vX.Y.Z bash
depsilo start --daemon
```

**Database migrations.** A new version migrates the database at its first
start and logs one `running database migration` line for each step. A
release that does not change the schema logs none. Migrations only go
forward. An older binary refuses a database that a newer one has migrated.
It stops at startup, and the error contains:

```text
database schema version <n> is newer than this binary supports (<m>); upgrade Depsilo instead of downgrading the database
```

To go back to the older version, stop Depsilo, install the older binary or
image, and [restore](#back-up-and-restore) the backup from step 1. Changes
made after that backup are lost.

A v0.9.0 daemon needs [its own procedure](#upgrade-a-v090-daemon) before
this one applies.
An older deployment on a named volume may need
[Other older named-volume layouts](#other-older-named-volume-layouts) first.

## Uninstall

Removing Depsilo has three parts: the service, its state, and the settings on
client machines that point at it. Take a [backup](#back-up-and-restore) first
if you may want the instance back.

**Container.** Remove the container, then the volume. The volume holds the
configuration, the database and the caches:

```bash
docker rm -f depsilo
docker volume rm depsilo-data
```

With Compose, `docker compose down -v` removes the container, the network and
the volume. For [bind-mounted state](#bind-mounted-state), delete the host
directory instead of a volume.

**Binary.**

1. Stop it: `depsilo stop` for the daemon, Ctrl+C for `depsilo serve`.
2. Delete the binary. The installer put it in `/usr/local/bin`, or in the
   directory given as `DEPSILO_INSTALL_DIR`.
3. Delete the state root `~/.depsilo`, and any path you moved out of it with
   the settings under [Overrides](#overrides).
4. Delete `~/.local/share/depsilo`, which holds the daemon's `depsilo.pid`,
   `depsilo.log` and `depsilo.lock`.
5. Delete `~/.config/depsilo/token` if you created it for the CLI.

**Tray application.** On Linux, run the installer from the extracted release
archive:

```bash
bash depsilo-tray/install-linux.sh uninstall
```

It removes the binary, the launcher, the icon and the autostart entry. On
macOS, quit the application and delete `Depsilo.app`. The state in
`~/.depsilo` stays in both cases; delete it as for the binary.

**The block that `depsilo init-agent` wrote.** The command wrote one block
into `AGENTS.md`, `CLAUDE.md` or `.cursorrules` of each project. Delete the
block together with its two marker lines:

| File | First line of the block | Last line of the block |
| --- | --- | --- |
| `AGENTS.md`, `CLAUDE.md` | `<!-- depsilo:start -->` | `<!-- depsilo:end -->` |
| `.cursorrules` | `# depsilo:start` | `# depsilo:end` |

A file that the command created, and that nobody edited since, holds only a
heading line besides the block; delete the file. If you registered the MCP
endpoint in Claude Code, remove it with `claude mcp remove depsilo`, and
revoke the API token in System management → Users.

**Client settings.** A client that still points at a removed instance fails
every install. A setting given as an option or as an environment variable of
one command needs no undo. The commands below undo the settings that the
Portal's commands store for the user:

| Client | The Portal's command | Undo |
| --- | --- | --- |
| pip | `pip config set global.index-url`, and `pip config set global.trusted-host` for a plain-HTTP address | `pip config unset global.index-url`, and `pip config unset global.trusted-host` if it was set |
| PDM | `pdm config pypi.url` | `pdm config -d pypi.url` |
| Conda | `conda config --set repodata_use_zst false` | `conda config --remove-key repodata_use_zst` |
| npm | `npm config set registry` | `npm config delete registry` |
| pnpm | `pnpm config set registry` | `pnpm config delete registry` |
| Yarn 1 | `yarn config set registry` | `yarn config delete registry` |
| Go | `go env -w GOPROXY=...` | `go env -u GOPROXY`, and `go env -u GOSUMDB` if you set that too |
| Bundler | `bundle config set --global mirror.https://rubygems.org` | `bundle config unset --global mirror.https://rubygems.org` |
| Helm | `helm repo add depsilo` | `helm repo remove depsilo` |
| .NET | `dotnet nuget add source ... -n depsilo` | `dotnet nuget remove source depsilo` |
| Composer | `composer config -g repo.packagist` and `composer config -g secure-http false` | `composer config -g --unset repo.packagist`, then `composer config -g --unset secure-http` |

Notes on the table:

- pip: **Connect a client** writes the two settings with `--user`, or with
  `--site` for a virtual environment. Add the same flag to the undo, for
  example `python3 -m pip config --user unset global.index-url` (`py` on
  Windows).
- Bundler: for a project, the commands are
  `bundle config set --local mirror.https://rubygems.org` and
  `bundle config unset --local mirror.https://rubygems.org`. Without a
  flag, `bundle config unset mirror.https://rubygems.org` removes the
  setting from the project and from the user's settings. Bundler 1.x has no
  `set` and `unset`: there the Portal gives
  `bundle config --local mirror.https://rubygems.org` (or `--global`), and
  `bundle config unset` exits with 0, removes nothing, and stores a
  meaningless `unset` setting instead. The undo on 1.x is
  `bundle config --delete mirror.https://rubygems.org`, which removes the
  setting from the project and from the user's settings.
- Three of the Portal's commands change the project and not the user's
  settings. Undo them in the project: `poetry source remove depsilo` for
  Poetry, `yarn config unset npmRegistryServer` for Yarn 2+ (and
  `yarn config unset unsafeHttpWhitelist` if you set that for a plain-HTTP
  address), and the Bundler `--local` form above.
- apt: the Portal's command rewrites
  `/etc/apt/sources.list.d/debian.sources`. Put the original addresses back
  and run `sudo apt update`. If the file named `http://deb.debian.org`
  before, this does it, with `<url>` standing for the Depsilo address:

  ```bash
  sudo sed -i -e 's|<url>/apt/debian-security|http://deb.debian.org/debian-security|g' -e 's|<url>/apt/debian|http://deb.debian.org/debian|g' /etc/apt/sources.list.d/debian.sources
  ```

- Settings that you wrote into a file by hand stay until you remove them:
  for example `pip.conf`, `.npmrc`, Maven's `settings.xml`, `.condarc`,
  `/etc/docker/daemon.json`, and `export` lines in a shell profile.
