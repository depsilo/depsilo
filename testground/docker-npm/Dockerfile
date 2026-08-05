FROM node:20-alpine@sha256:fb4cd12c85ee03686f6af5362a0b0d56d50c58a04632e6c0fb8363f609372293

ARG DEPSILO_URL

RUN npm config set registry "${DEPSILO_URL}/npm/" \
    && mkdir -p /test && cd /test \
    && npm init -y >/dev/null \
    && npm install --no-audit --no-fund --no-progress lodash@4.17.21

RUN node -e "const _ = require('/test/node_modules/lodash'); console.log('npm:', 'lodash', _.VERSION, 'OK')"
