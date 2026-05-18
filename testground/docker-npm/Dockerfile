FROM node:20-alpine

ARG NPM_REGISTRY

RUN npm config set registry "${NPM_REGISTRY}" \
    && mkdir -p /test && cd /test \
    && npm init -y >/dev/null \
    && npm install --no-audit --no-fund --no-progress lodash@4.17.21

RUN node -e "const _ = require('/test/node_modules/lodash'); console.log('npm:', 'lodash', _.VERSION, 'OK')"
