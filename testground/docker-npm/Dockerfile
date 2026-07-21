FROM node:20-alpine

ARG DEPSILO_URL

RUN npm config set registry "${DEPSILO_URL}/npm/" \
    && mkdir -p /test && cd /test \
    && npm init -y >/dev/null \
    && npm install --no-audit --no-fund --no-progress lodash@4.17.21

RUN node -e "const _ = require('/test/node_modules/lodash'); console.log('npm:', 'lodash', _.VERSION, 'OK')"
