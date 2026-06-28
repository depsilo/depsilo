# Package Mirror Integration Prompt

> This file is the canonical prompt the Portal renders for users to paste into
> their coding LLM (Claude Code, Cursor, Copilot Chat, …). Placeholders are
> substituted at render time:
>
> - `{{MIRROR_URL}}` → the running service's public URL (e.g. `http://10.4.20.52:23333`)
>
> The text below `---` is what reaches the LLM. Keep it transparent,
> security-tool-discipline, prescriptive. Edits here ship to every user
> immediately. **Never re-introduce stealth/branding-discipline language** —
> a self-hosted supply-chain control point must be honest about what it did
> to a codebase. See `docs/adr/0003-supply-chain-control-point.md`.

---

You are integrating **Depsilo**, a self-hosted package cache + supply-chain
control point, into this project. Goal: route the project's dependency-install
traffic through `{{MIRROR_URL}}` so cold builds hit a local cache instead of the
public CDN every time, **while keeping the public registry available as a
documented fallback**.

## Identification — every change must be traceable

Every config edit you make must name Depsilo and the mirror URL in a comment or
label adjacent to the change, so a future reviewer or `git blame` can answer
"what is this URL and why is it here?" in five seconds. Examples:

```bash
# Routed via Depsilo cache: {{MIRROR_URL}}
ARG NPM_REGISTRY={{MIRROR_URL}}/npm/
```

```toml
# Routed via Depsilo cache: {{MIRROR_URL}}
[source.crates-io]
replace-with = "depsilo"
```

```yaml
# .github/workflows/build.yml
env:
  PIP_INDEX_URL: {{MIRROR_URL}}/pypi/simple/  # routed via Depsilo cache
```

When you commit, suggest a subject line that names the tool, e.g.:

```
chore(deps): route install traffic through Depsilo cache ({{MIRROR_URL}})
```

This identification rule has no exceptions. If you cannot add a traceable
comment in the file's syntax (e.g. binary JSON), surface that file in the
report at the end instead of editing it silently.

## How to work

1. **Discover before editing.** List dependency-related files first:
   `Dockerfile*`, `docker-compose*.y*ml`, `.github/workflows/`,
   `.gitlab-ci.y*ml`, `azure-pipelines.y*ml`, `Jenkinsfile`,
   `Makefile`, `build.sh`, `package.json`, `pnpm-workspace.yaml`,
   `pyproject.toml`, `requirements*.txt`, `go.mod`, `Cargo.toml`,
   `pom.xml`, `Gemfile`, `composer.json`, `*.csproj`, `environment.yml`,
   `DESCRIPTION`, `Chart.yaml`, `.npmrc`, `pip.conf`, `.condarc`,
   `nuget.config`, `Rprofile.site`.

2. **Reuse existing knobs.** If the project already exposes a mirror-like
   build-arg (`NPM_REGISTRY`, `ALPINE_MIRROR`, `--cn-mirror`, etc.) feed it the
   new value. Do **not** add a parallel mechanism.

3. **Inject at build time, not in checked-in dev configs.** Prefer build-args,
   CI variables, and `ENV` lines in Dockerfiles. Do not commit a personal
   `.npmrc` / `pip.conf` / `~/.cargo/config.toml` to the repo root unless that
   project already does.

4. **Be idempotent.** If the URL is already wired for an ecosystem, skip it.
   If a previous Depsilo integration edit is present (look for the
   `# Routed via Depsilo cache` marker), refresh the URL if needed but do not
   duplicate the line.

5. **Stay in scope.** Do not refactor surrounding code, do not "tidy up"
   unrelated files, do not modify lockfiles. If multiple competing patterns
   exist or you find more than three ecosystems, stop and ask the user which to
   wire first.

## Per-ecosystem endpoints

Append the path below to `{{MIRROR_URL}}`. Use whichever delivery vehicle (env
var, config file, build-arg) matches the project's existing style. The "native
knob" column lists the language's own variable / config syntax — use those
names verbatim, do not invent new ones.

| Ecosystem                     | URL path                            | Native knob                                                                               |
| ----------------------------- | ----------------------------------- | ----------------------------------------------------------------------------------------- |
| npm / pnpm / yarn             | `/npm/`                             | env `npm_config_registry`; or `registry=` in `.npmrc`                                     |
| Python (pip / uv / poetry)    | `/pypi/simple/`                     | env `PIP_INDEX_URL` (+ `PIP_TRUSTED_HOST=<host>`); or `[global] index-url = …` in `pip.conf` |
| Go modules                    | `/go`                               | env `GOPROXY="<URL>/go,direct"`                                                           |
| Rust / Cargo                  | `/crates/`                          | `~/.cargo/config.toml` `[source.crates-io] replace-with = "depsilo"` + `[source.depsilo] registry = "sparse+<URL>/crates/"` |
| Maven / Gradle                | `/maven/`                           | `~/.m2/settings.xml` `<mirrors><mirror><mirrorOf>*</mirrorOf><name>depsilo</name><url>…</url></mirror></mirrors>` |
| Ruby / Bundler                | `/rubygems/`                        | `bundle config mirror.https://rubygems.org <URL>`; or `~/.bundle/config`                  |
| PHP / Composer                | `/composer/`                        | `composer config -g repo.packagist composer <URL>`; or project `repositories` block       |
| .NET / NuGet                  | `/nuget/v3/index.json`              | `nuget.config` `<packageSources>` entry named `depsilo`                                   |
| Conda / Mamba                 | `/conda/`                           | `~/.condarc` channels                                                                     |
| R / CRAN                      | `/cran/`                            | `Rprofile.site` `options(repos = c(CRAN = "<URL>"))`                                      |
| Helm                          | `/helm/`                            | `helm repo add depsilo <URL>`                                                             |
| Debian / Ubuntu (apt)         | `/apt/<distro>` (e.g. `/apt/debian`) | rewrite `/etc/apt/sources.list` to point at the URL; do not disable signature checking    |
| Alpine (apk)                  | `/alpine/<version>/<repo>`           | rewrite `/etc/apk/repositories`                                                           |
| Docker registry mirror        | `/docker/`                          | `/etc/docker/daemon.json` `registry-mirrors` (host-level; only suggest, do not edit the user's daemon config) |
| HuggingFace                   | `/huggingface/`                     | env `HF_ENDPOINT=<URL>/huggingface`                                                       |

## Public-registry fallback — always preserved

The mirror is a **primary route**, never a sole route. Configure the public
registry as a documented fallback so a Depsilo outage or a misconfigured URL
cannot brick the build pipeline:

- **Go modules.** `GOPROXY="{{MIRROR_URL}}/go,direct"` — the `,direct` suffix is
  the fallback. Required, do not omit.
- **Cargo.** Cargo's `[source.crates-io] replace-with = "depsilo"` only swaps
  the registry, the original `crates-io` source remains available; nothing to
  do beyond keeping the original `[source.crates-io]` block intact.
- **Maven.** `<mirrorOf>*</mirrorOf>` routes everything via Depsilo, but Maven
  falls back to the original `<repository>` if the mirror returns 404 — do not
  set `<blocked>true</blocked>` on the mirror.
- **npm / pnpm / yarn.** The native registry knob takes a single URL, so the
  fallback lives at the CI level: in CI configs, set the mirror URL via env
  but leave a documented commented-out line restoring the public registry, so
  bringing the pipeline back up during an outage is a one-line revert.
- **pip.** Keep `PIP_EXTRA_INDEX_URL=https://pypi.org/simple/` set alongside
  `PIP_INDEX_URL` so pip falls back when the mirror cannot satisfy a request.
- **apt / apk.** Leave the public mirror line commented in `sources.list` /
  `repositories` rather than deleted, with a comment pointing at this prompt.

If the project has an explicit reason to enforce mirror-only (air-gapped
environments, government / regulated targets where every dependency must come
from a controlled mirror), the user will tell you. Do not assume.

## Hard constraints

- **Never** set a global `HTTP_PROXY` / `HTTPS_PROXY` to redirect traffic. The
  URL is not a forward proxy; it serves only the per-ecosystem paths above.
- **Never** disable signature / GPG / checksum verification
  (`--allow-unauthenticated`, `--no-verify`, `--insecure-registry`,
  `GOSUMDB=off` for projects that don't already set it, etc.). The mirror
  preserves upstream signatures unchanged.
- **Never** modify lockfiles. The mirror serves the same artifacts as the
  public registry; integrity hashes still match.
- **Never** hardcode the URL inside files that get published to a registry
  (e.g. inside a published `package.json` `publishConfig`, a `setup.py`).
  Build-time injection only.
- **Never** rename or remove the project's existing mirror knobs (`--cn-mirror`,
  `ALPINE_MIRROR`, `NPM_REGISTRY`, etc.). Feed them the new value.
- **Never** introduce a new dependency, package, or wrapper script just to
  perform the injection. The native env / config files are sufficient.
- **Never** remove a public-registry source entry. Replace it as primary but
  keep it available — see the fallback section above.

## Optional: throttle for cold-cache fetch storms

Only add this when you can see the project's install step uses high
concurrency (pnpm in monorepo, parallel pip wheels, etc.) **and** the build
will run cold-cache (Docker `FROM` with no prior layer, fresh CI runner). In
those cases the first build can saturate the mirror's upstream pull and
fetcher sockets time out. Conservative defaults:

- pnpm: in a project-level `.npmrc`, add `network-concurrency=4`,
  `fetch-timeout=600000`, `fetch-retries=5`.
- pip: set env `PIP_DEFAULT_TIMEOUT=600`.
- cargo: set env `CARGO_HTTP_TIMEOUT=600`, `CARGO_HTTP_MULTIPLEXING=false`.
- yarn (classic): `yarn config set network-concurrency 4`.

Do not add these preemptively.

## What to report back

After editing, print a markdown table at the bottom of your reply with one row
per file you touched:

| File | Ecosystem | Action | Why |
|------|-----------|--------|-----|
| `Dockerfile` | npm | Added `ENV NPM_REGISTRY={{MIRROR_URL}}/npm/` + Depsilo identification comment | route npm install through cache |
| `pip.conf` | pip | Set `index-url` + kept public PyPI as `extra-index-url` | route pip with documented fallback |

Then:

1. Suggest one verification command per ecosystem touched, e.g.:
   - Docker: `docker build … && docker history <image> | grep -iE "registry|index_url|GOPROXY"` should show the Depsilo URL baked in.
   - Local Python: `pip config list` should show the new `index-url` plus the
     PyPI `extra-index-url`.
   - npm: `npm config get registry` inside the container should print the
     Depsilo URL.
2. Surface anything you skipped and why (e.g. "lockfile present, not touched";
   "two competing CN-mirror flags — confirm preference"; "binary config file,
   could not add identification comment, please verify manually").
3. Suggest the commit subject:
   ```
   chore(deps): route install traffic through Depsilo cache ({{MIRROR_URL}})
   ```
4. Do **not** run the build yourself unless the user explicitly asks.

## When to stop and ask

- The project has two or more competing mirror toggles and it isn't obvious
  which one is canonical.
- The deploy target probably can't reach `{{MIRROR_URL}}` (e.g. the build
  runs on GitHub-hosted runners or in a separate VPC). Surface the risk; do
  not silently wire it up.
- The project publishes to a registry. Confirm you should only touch
  install-side, never publish-side, config.
- The user has asked for mirror-only (no public fallback). Confirm the
  enforcement context (air-gapped, regulated, etc.) before removing fallback
  entries.
