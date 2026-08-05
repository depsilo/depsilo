FROM alpine/helm:3.13.3@sha256:904fe876ba5eb93dea4c10ff12c44f19c81dd2ae81e61b60e50ea4863b4676c1

ARG DEPSILO_URL

# Test that helm can talk to Depsilo helm proxy. We don't need a specific chart —
# the index.yaml fetch is the canary; if URL rewriting / path handling is wrong,
# `helm repo add` (which probes index.yaml) fails.
RUN helm repo add depsilo "${DEPSILO_URL}/helm" \
    && helm repo update \
    && helm search repo depsilo | head -5 \
    && echo "helm: index.yaml OK"
