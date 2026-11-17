# Real npm, pnpm, Yarn 1, Yarn 4 and Bun clients against Depsilo's npm route.
# Each client reads its registry from the project file the Portal generates for
# it, installs lodash once (fresh) and once more from the lockfile that install
# wrote (frozen). Every install starts with an empty client cache; see lib.sh.
FROM node:20-alpine@sha256:fb4cd12c85ee03686f6af5362a0b0d56d50c58a04632e6c0fb8363f609372293

ARG DEPSILO_URL
ENV DEPSILO_NPM=${DEPSILO_URL}/npm/ \
    NPM_CONFIG_UPDATE_NOTIFIER=false \
    PATH=/opt/pnpm/node_modules/.bin:/opt/bun/node_modules/.bin:$PATH

COPY lib.sh /fixture/lib.sh
COPY --chown=node:node existing-lockfile /test/npm-existing-lockfile

# npm is the one bundled with the Node image; the other client versions are
# pinned here. They are installed through Depsilo, which also covers a scoped
# package (@yarnpkg/cli-dist) and the platform-specific optional dependencies
# that carry the pnpm and Bun executables. Yarn 1 and Yarn 4 both provide a
# "yarn" command, so each client gets its own prefix and the two Yarns get
# distinct names. The Yarn 1 that the Node image ships is not used.
RUN . /fixture/lib.sh \
    && for client in pnpm=pnpm@12.21.0 yarn-classic=yarn@1.22.22 \
        yarn-berry=@yarnpkg/cli-dist@4.18.1 bun=bun@1.4.4; do \
      npm install --registry "${DEPSILO_NPM}" --no-audit --no-fund --no-progress \
        --prefix "/opt/${client%%=*}" "${client#*=}" \
      && through_depsilo "/opt/${client%%=*}/package-lock.json" || exit 1; \
    done \
    && ln -s /opt/yarn-classic/node_modules/.bin/yarn /usr/local/bin/yarn-classic \
    && ln -s /opt/yarn-berry/node_modules/.bin/yarn /usr/local/bin/yarn-berry \
    && chown node:node /test \
    && echo "npm $(npm --version): installed pnpm $(pnpm --version), Yarn $(yarn-classic --version), Yarn $(yarn-berry --version) and Bun $(bun --version) through Depsilo OK"

# The install steps must not run as root; see cold_start in lib.sh.
USER node
WORKDIR /test

# npm reads the registry line of .npmrc.
RUN . /fixture/lib.sh && mkdir npm && cd npm \
    && echo "registry=${DEPSILO_NPM}" > .npmrc \
    && npm init -y > /dev/null \
    && cold_start && npm install --no-audit --no-fund --no-progress lodash@4.17.21 \
    && through_depsilo package-lock.json && lodash_loads \
    && echo "npm $(npm --version): fresh install of lodash 4.17.21 OK"
RUN . /fixture/lib.sh && cd npm && rm -rf node_modules \
    && cold_start && npm ci --no-audit --no-fund --no-progress \
    && lodash_loads \
    && echo "npm $(npm --version): npm ci from the lockfile of that install OK"

# A project that already has a lockfile: existing-lockfile/package-lock.json
# was recorded against https://registry.npmjs.org. npm replaces the host of
# each resolved URL with the configured registry and asks Depsilo for the
# conventional tarball path, which Depsilo refuses because it serves only the
# signed URLs it writes into package metadata. The remedy is the two steps the
# Portal gives: rewrite the lockfile without resolved URLs, then install.
RUN . /fixture/lib.sh && cd npm-existing-lockfile \
    && echo "registry=${DEPSILO_NPM}" > .npmrc \
    && cold_start \
    && if npm ci --no-audit --no-fund --no-progress > /tmp/npm-ci.log 2>&1; then \
         fail "npm ci accepted a lockfile recorded against registry.npmjs.org"; \
       fi \
    && cat /tmp/npm-ci.log \
    && unsigned_tarball_refused /tmp/npm-ci.log left-pad/-/left-pad-1.3.0.tgz \
    && echo "npm $(npm --version): npm ci with a lockfile recorded against registry.npmjs.org is refused with NPM_UNSIGNED_TARBALL OK"
RUN . /fixture/lib.sh && cd npm-existing-lockfile \
    && cold_start \
    && npm install --package-lock-only --omit-lockfile-registry-resolved --no-audit --no-fund --no-progress \
    && without_resolved_urls package-lock.json \
    && cold_start && npm ci --no-audit --no-fund --no-progress \
    && node -e "require('left-pad')" \
    && echo "npm $(npm --version): npm ci after rewriting that lockfile without resolved URLs OK"

# pnpm reads the registry line of .npmrc, as npm does. pnpm 11 and later ignore
# NPM_CONFIG_REGISTRY, so the file is the setting both generations share.
RUN . /fixture/lib.sh && mkdir pnpm && cd pnpm \
    && echo "registry=${DEPSILO_NPM}" > .npmrc \
    && echo '{"name":"depsilo-pnpm","private":true}' > package.json \
    && cold_start && pnpm add lodash@4.17.21 \
    && through_depsilo pnpm-lock.yaml && lodash_loads \
    && echo "pnpm $(pnpm --version): fresh install of lodash 4.17.21 OK"
# trustLockfile is the setting the Portal names for pnpm 11 and later. Depsilo
# signs a new tarball URL into every metadata response, so the URL in the
# lockfile never equals the one pnpm reads back, and without the setting the
# frozen install stops with ERR_PNPM_TARBALL_URL_MISMATCH.
RUN . /fixture/lib.sh && cd pnpm && rm -rf node_modules \
    && echo 'trustLockfile: true' > pnpm-workspace.yaml \
    && cold_start && pnpm install --frozen-lockfile \
    && lodash_loads \
    && echo "pnpm $(pnpm --version): frozen install from the lockfile of that install (trustLockfile) OK"

# Yarn 1 installs from "registry" in .yarnrc. It accepts npmRegistryServer and
# echoes it back, but does not use it.
RUN . /fixture/lib.sh && mkdir yarn-classic && cd yarn-classic \
    && echo "registry \"${DEPSILO_NPM}\"" > .yarnrc \
    && echo '{"name":"depsilo-yarn-classic","private":true}' > package.json \
    && cold_start && yarn-classic add lodash@4.17.21 \
    && through_depsilo yarn.lock && lodash_loads \
    && echo "Yarn $(yarn-classic --version): fresh install of lodash 4.17.21 OK"
RUN . /fixture/lib.sh && cd yarn-classic && rm -rf node_modules \
    && cold_start && yarn-classic install --frozen-lockfile \
    && lodash_loads \
    && echo "Yarn $(yarn-classic --version): frozen install from the lockfile of that install OK"

# Yarn 2 and later read npmRegistryServer from .yarnrc.yml. On plain HTTP they
# also need the host name in unsafeHttpWhitelist, without the port: Yarn
# rejects an entry that carries one. The default linker is Plug'n'Play, so
# there is no node_modules and lodash is loaded through "yarn node".
RUN . /fixture/lib.sh && mkdir yarn-berry && cd yarn-berry \
    && hostname="${DEPSILO_URL#*://}" && hostname="${hostname%%[:/]*}" \
    && printf '%s\n' "npmRegistryServer: \"${DEPSILO_NPM}\"" \
        'unsafeHttpWhitelist:' "  - \"${hostname}\"" > .yarnrc.yml \
    && echo '{"name":"depsilo-yarn-berry","private":true}' > package.json \
    && cold_start && yarn-berry add lodash@4.17.21 \
    && through_depsilo yarn.lock && lodash_loads yarn-berry \
    && echo "Yarn $(yarn-berry --version): fresh install of lodash 4.17.21 OK"
RUN . /fixture/lib.sh && cd yarn-berry && rm -rf .yarn .pnp.cjs .pnp.loader.mjs \
    && cold_start && yarn-berry install --immutable \
    && lodash_loads yarn-berry \
    && echo "Yarn $(yarn-berry --version): immutable install from the lockfile of that install OK"

# Bun reads [install] registry from bunfig.toml.
RUN . /fixture/lib.sh && mkdir bun && cd bun \
    && printf '%s\n' '[install]' "registry = \"${DEPSILO_NPM}\"" > bunfig.toml \
    && echo '{"name":"depsilo-bun","private":true}' > package.json \
    && cold_start && bun add lodash@4.17.21 \
    && through_depsilo bun.lock && lodash_loads \
    && echo "Bun $(bun --version): fresh install of lodash 4.17.21 OK"
RUN . /fixture/lib.sh && cd bun && rm -rf node_modules \
    && cold_start && bun install --frozen-lockfile \
    && lodash_loads \
    && echo "Bun $(bun --version): frozen install from the lockfile of that install OK"
