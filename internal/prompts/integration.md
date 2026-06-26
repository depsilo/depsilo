# Package Mirror Integration Prompt

> This file is the canonical prompt the Portal renders for users to paste into
> their coding LLM (Claude Code, Cursor, Copilot Chat, …). Placeholders are
> substituted at render time:
>
> - `{{MIRROR_URL}}` → the running service's public URL (e.g. `http://10.4.20.52:23333`)
>
> The text below `---` is what reaches the LLM. Keep it brand-neutral, terse,
> prescriptive. Edits here ship to every user immediately.

---

You are integrating a private package mirror into this project. Goal: route the
project's dependency-install traffic through `{{MIRROR_URL}}` so cold builds use
a local cache and the project no longer depends on direct public CDN access.

**Brand discipline.** Do not write the mirror's product name, hostname, or any
identifying string into committed source. Treat `{{MIRROR_URL}}` as an opaque
internal address. The only thing that should appear in committed files is a
generic build-arg / env var (e.g. `NPM_REGISTRY`, `PIP_INDEX_URL`,
`MIRROR_URL`) whose default may point at the URL, plus the language's own
native config syntax.

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
| Rust / Cargo                  | `/crates/`                          | `~/.cargo/config.toml` `[source.crates-io] replace-with = "mirror"` + `[source.mirror] registry = "sparse+<URL>/crates/"` |
| Maven / Gradle                | `/maven/`                           | `~/.m2/settings.xml` `<mirrors><mirror><mirrorOf>*</mirrorOf><url>…</url></mirror></mirrors>` |
| Ruby / Bundler                | `/rubygems/`                        | `bundle config mirror.https://rubygems.org <URL>`; or `~/.bundle/config`                  |
| PHP / Composer                | `/composer/`                        | `composer config -g repo.packagist composer <URL>`; or project `repositories` block       |
| .NET / NuGet                  | `/nuget/v3/index.json`              | `nuget.config` `<packageSources>` entry                                                   |
| Conda / Mamba                 | `/conda/`                           | `~/.condarc` channels                                                                     |
| R / CRAN                      | `/cran/`                            | `Rprofile.site` `options(repos = c(CRAN = "<URL>"))`                                      |
| Helm                          | `/helm/`                            | `helm repo add <local-name> <URL>` (pick a neutral `<local-name>`)                        |
| Debian / Ubuntu (apt)         | `/apt/<distro>` (e.g. `/apt/debian`) | rewrite `/etc/apt/sources.list` to point at the URL; do not disable signature checking    |
| Alpine (apk)                  | `/alpine/<version>/<repo>`           | rewrite `/etc/apk/repositories`                                                           |
| Docker registry mirror        | `/docker/`                          | `/etc/docker/daemon.json` `registry-mirrors` (host-level; only suggest, do not edit the user's daemon config) |
| HuggingFace                   | `/huggingface/`                     | env `HF_ENDPOINT=<URL>/huggingface`                                                       |

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

After editing:

1. List each file you changed and which ecosystem each handles.
2. Suggest one verification command per ecosystem touched, e.g.:
   - Docker: `docker build … && docker history <image> | grep -iE "registry|index_url|GOPROXY"` should show the URL baked in.
   - Local Python: `pip config list` should show the new `index-url`.
   - npm: `npm config get registry` inside the container should print the URL.
3. Surface anything you skipped and why (e.g. "lockfile present, not touched";
   "two competing CN-mirror flags — confirm preference").
4. Do **not** run the build yourself unless the user explicitly asks.

## When to stop and ask

- The project has two or more competing mirror toggles and it isn't obvious
  which one is canonical.
- The deploy target probably can't reach `{{MIRROR_URL}}` (e.g. the build
  runs on GitHub-hosted runners or in a separate VPC). Surface the risk; do
  not silently wire it up.
- The project publishes to a registry. Confirm you should only touch
  install-side, never publish-side, config.
