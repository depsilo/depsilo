# Package Mirror Integration Prompt

> This file is the canonical prompt the Portal renders for users to paste into
> their coding LLM (Claude Code, Cursor, Copilot Chat, …). Placeholders are
> substituted at render time:
>
> - `{{MIRROR_URL}}` → the running service's public URL (e.g. `http://depsilo.example:23333`)
>
> The text below `---` is what reaches the LLM. Keep it transparent,
> security-tool-discipline, prescriptive. Edits here ship to every user
> immediately. **Never re-introduce stealth/branding-discipline language** —
> a self-hosted supply-chain enforcement layer must be honest about what it did
> to a codebase. See `docs/adr/0003-supply-chain-control-point.md`.

---

You are integrating **Depsilo**, a self-hosted package cache + supply-chain
enforcement layer, into this project. Goal: route the project's dependency-install
traffic through `{{MIRROR_URL}}` so cold builds hit a local cache instead of the
public CDN every time, **while preserving the original public-registry setting
as a documented rollback, not an active parallel source**.

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

2. **Present a plan and wait.** After discovery and before changing any file,
   list every file you intend to change and the edit for each, worked out
   under the rules below. Then stop and wait for the user to confirm. If
   nobody can confirm (a non-interactive run), stop after presenting the plan.

3. **Reuse existing knobs.** If the project already exposes a mirror-like
   build-arg (`NPM_REGISTRY`, `ALPINE_MIRROR`, `--cn-mirror`, etc.) feed it the
   new value. Do **not** add a parallel mechanism.

4. **Inject at build time, not in checked-in dev configs.** Prefer build-args,
   CI variables, and `ENV` lines in Dockerfiles. Do not commit a personal
   `.npmrc` / `pip.conf` / `~/.cargo/config.toml` to the repo root unless that
   project already does. Where the table below gives a client only a project
   file, use that file and list it in the plan, unless the project publishes
   it (a library's `pyproject.toml`): then leave it and report.

5. **Be idempotent.** If the URL is already wired for an ecosystem, skip it.
   If a previous Depsilo integration edit is present (look for the
   `# Routed via Depsilo cache` marker), refresh the URL if needed but do not
   duplicate the line.

6. **Stay in scope.** Do not refactor surrounding code, do not "tidy up"
   unrelated files, do not modify lockfiles (the one exception is under "Hard
   constraints"). If multiple competing patterns exist or you find more than
   three ecosystems, stop and ask the user which to wire first.

## Per-ecosystem endpoints

Append the path below to `{{MIRROR_URL}}`. Use whichever delivery vehicle (env
var, config file, build-arg) matches the project's existing style. The "native
knob" column lists the setting each client reads — use those names verbatim;
do not invent new ones or carry one client's setting over to another.

| Ecosystem                     | URL path                            | Native knob                                                                               |
| ----------------------------- | ----------------------------------- | ----------------------------------------------------------------------------------------- |
| npm                           | `/npm/`                             | `registry=<URL>/npm/` in the project `.npmrc`; or env `npm_config_registry`               |
| pnpm                          | `/npm/`                             | `registry=<URL>/npm/` in the project `.npmrc` (every version); or env `PNPM_CONFIG_REGISTRY` for pnpm 11 and later (they ignore `npm_config_registry`), `npm_config_registry` for pnpm 10 and earlier |
| Yarn 2+                       | `/npm/`                             | `npmRegistryServer: "<URL>/npm/"` in `.yarnrc.yml`; for plain HTTP also list the host name, without the port, under `unsafeHttpWhitelist:` |
| Yarn 1                        | `/npm/`                             | `registry "<URL>/npm/"` in `.yarnrc`                                                      |
| Bun                           | `/npm/`                             | `[install] registry = "<URL>/npm/"` in `bunfig.toml`                                      |
| pip                           | `/pypi/simple/`                     | env `PIP_INDEX_URL`; add `PIP_TRUSTED_HOST=<host>` only for plain HTTP, never HTTPS; or `[global] index-url = …` in `pip.conf` |
| uv                            | `/pypi/simple/`                     | env `UV_INDEX_URL`; or `[[tool.uv.index]]` with `name = "depsilo"`, `url = "<URL>/pypi/simple/"`, `default = true` in `pyproject.toml` |
| Poetry                        | `/pypi/simple/`                     | `[[tool.poetry.source]]` with `name = "depsilo"`, `url = "<URL>/pypi/simple/"`, `priority = "primary"` in `pyproject.toml` |
| PDM                           | `/pypi/simple/`                     | `[[tool.pdm.source]]` with `name = "pypi"`, `url = "<URL>/pypi/simple/"` in `pyproject.toml`; add `verify_ssl = false` only for plain HTTP |
| Pipenv                        | `/pypi/simple/`                     | env `PIPENV_PYPI_MIRROR`; or `[[source]]` with `name = "depsilo"`, `url = "<URL>/pypi/simple/"` in `Pipfile`, plus `verify_ssl = false` only for plain HTTP |
| Go modules                    | `/go`                               | env `GOPROXY="<URL>/go,direct"`                                                           |
| Rust / Cargo                  | `/crates/`                          | `~/.cargo/config.toml` `[source.crates-io] replace-with = "depsilo"` + `[source.depsilo] registry = "sparse+<URL>/crates/"` |
| Maven                         | `/maven/`                           | `~/.m2/settings.xml` `<mirrors><mirror><id>depsilo</id><mirrorOf>*</mirrorOf><url>…</url></mirror></mirrors>` |
| Gradle                        | `/maven/`                           | `maven { url "<URL>/maven/" }` in the `repositories` block of the build file, or of `allprojects` in `~/.gradle/init.gradle`; for plain HTTP also `allowInsecureProtocol = true` (Kotlin DSL: `isAllowInsecureProtocol`) |
| sbt                           | `/maven/`                           | `[repositories]` in `~/.sbt/repositories` with `depsilo: <URL>/maven/` and `local`; run sbt with `-Dsbt.override.build.repos=true` |
| RubyGems (`gem`)              | `/rubygems/`                        | `gem install --source <URL>/rubygems/`; or `:sources:` in `.gemrc`                        |
| Bundler                       | `/rubygems/`                        | `bundle config set --local mirror.https://rubygems.org <URL>/rubygems/` writes the project's `.bundle/config`; `--global` writes `~/.bundle/config`; check `bundle --version` first: before 2.1 leave out `set` |
| PHP / Composer                | `/composer/`                        | `composer config -g repo.packagist composer <URL>/composer/`; or project `repositories` block; for plain HTTP also `composer config -g secure-http false` |
| .NET / NuGet                  | `/nuget/v3/index.json`              | `nuget.config` `<packageSources>` entry named `depsilo`; for plain HTTP also `allowInsecureConnections="true"` on it |
| Conda / Mamba                 | `/conda/`                           | `~/.condarc` channels                                                                     |
| R / CRAN                      | `/cran/`                            | `Rprofile.site` `options(repos = c(CRAN = "<URL>/cran/"))`                                |
| Helm                          | `/helm/`                            | `helm repo add depsilo <URL>/helm/`                                                       |
| Debian / Ubuntu (apt)         | `/apt/<distro>` (e.g. `/apt/debian`) | rewrite `/etc/apt/sources.list` to point at the URL; do not disable signature checking    |
| Alpine (apk)                  | `/alpine/<version>/<repo>`           | rewrite `/etc/apk/repositories`                                                           |
| Docker registry mirror        | service root; Docker requests `/v2/` | `/etc/docker/daemon.json` `registry-mirrors` (host-level; only suggest, do not edit the user's daemon config) |
| HuggingFace                   | `/huggingface/`                     | env `HF_ENDPOINT=<URL>/huggingface`                                                       |

## Public-registry fallback / recovery path — always preserved

The mirror is the **primary route**. Do not add a parallel index or all-error
fallthrough that can bypass a Depsilo policy response. Preserve the original
setting and a documented one-line rollback instead. Some clients have limited
continuation semantics, described below, but an install can still fail while
the client points at an unavailable Depsilo instance.

- **Go modules.** `GOPROXY="{{MIRROR_URL}}/go,direct"` uses `direct` only after
  a 404/410 response; it does not fail over when Depsilo is unreachable. Keep
  the original `GOPROXY` as the documented rollback. Never use `|direct`: it
  would also continue after Depsilo returns 451 and bypass enforcement.
- **Cargo.** `[source.crates-io] replace-with = "depsilo"` does not provide
  automatic outage failover. Keep the original `[source.crates-io]` block and
  document removing `replace-with` as the rollback.
- **Maven.** `<mirrorOf>*</mirrorOf>` routes matching repositories through
  Depsilo and does not guarantee automatic fallthrough on mirror failure. Keep
  the original repository settings and document disabling the mirror entry as
  the rollback; do not set `<blocked>true</blocked>`.
- **npm / pnpm / Yarn / Bun.** The native registry knob takes a single URL, so
  the fallback lives at the CI level: in CI configs, set the mirror URL but
  leave a documented commented-out line restoring the public registry, so
  bringing the pipeline back up during an outage is a one-line revert.
- **pip.** Do not set `PIP_EXTRA_INDEX_URL` as fallback: pip considers all
  configured indexes during normal resolution, which can bypass Depsilo policy
  and creates dependency-confusion risk. Keep a commented
  `PIP_INDEX_URL=https://pypi.org/simple/` rollback instead.
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
  preserves upstream signatures unchanged. Go checksum verification stays on
  and `GOSUMDB` needs no change: Depsilo relays checksum-database requests
  when a Go Upstream mirrors the database, and otherwise the go command asks
  sum.golang.org itself; only where that host is unreachable, propose
  `GOSUMDB=sum.golang.google.cn`.
- **Never** modify lockfiles, with one exception that the user approves as
  part of the plan. Depsilo's npm route serves only the signed tarball URLs it
  writes into package metadata; the conventional tarball paths an existing
  lockfile records return 404. A Node lockfile that records the public
  registry's URLs therefore fails through Depsilo wherever the client's own
  cache is cold (CI, container builds, new machines), even if it installs on
  a developer machine. npm follows URLs of a registry other than
  `registry.npmjs.org` as recorded: such a `package-lock.json` installs, but
  bypasses Depsilo.
  - **npm — the exception.** For a `package-lock.json` with `resolved` URLs
    of any registry (`registry.npmjs.org`: 404; any other: bypass), propose
    rewriting it with
    `npm install --package-lock-only --omit-lockfile-registry-resolved`. That
    drops the `resolved` URLs and keeps every `integrity` value; the result
    installs both through Depsilo and directly from the public registry.
    Passing the flag to `npm ci` or `npm install` alone, or only setting it in
    `.npmrc`, does not repair an existing lockfile. Also propose
    `omit-lockfile-registry-resolved=true` in the project `.npmrc`, so later
    installs do not record URLs again. A lockfile cannot carry the
    identification comment: name it in the report.
  - **pnpm / Yarn / Bun.** Do not edit or regenerate `pnpm-lock.yaml`,
    `yarn.lock`, `bun.lock` or `bun.lockb`. These clients have no equivalent
    flag, and a lockfile regenerated through Depsilo is tied to this instance
    (its address, signing secret and npm Upstream) and no longer installs
    from the public registry. Report the limitation to the user and let them
    decide. Yarn 1, and Bun with another registry's URLs, do not fail: they
    follow the recorded URLs and bypass Depsilo.
  - **uv / Poetry / PDM / Pipenv.** An index set in `pyproject.toml` or
    `Pipfile`, or through `UV_INDEX_URL`, makes `uv.lock`, `poetry.lock`,
    `pdm.lock` or `Pipfile.lock` stale, and a re-lock resolves it against
    this instance (a plain `uv sync` re-locks unasked). Do not re-lock: name
    the stale lockfile in the plan and let the user decide.
  - Every other lockfile stays untouched.
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
| `pip.conf` | pip | Set `index-url` + documented the original PyPI value for rollback | route pip through enforcement proxy |

Then:

1. Suggest one verification command per ecosystem touched, e.g.:
   - Docker: `docker build … && docker history <image> | grep -iE "registry|index_url|GOPROXY"` should show the Depsilo URL baked in.
   - Local Python: `pip config list` should show only the Depsilo `index-url`;
     verify the public PyPI URL remains in a rollback comment or runbook.
   - npm: `npm config get registry` inside the container should print the
     Depsilo URL.
2. Surface anything you skipped and why (e.g. "`pnpm-lock.yaml` records
   another registry — not touched, installs through Depsilo will return 404";
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
