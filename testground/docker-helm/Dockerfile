FROM alpine/helm:3.13.3

ARG HELM_REPO_URL

# Test that helm can talk to Depsilo helm proxy. We don't need a specific chart —
# the index.yaml fetch is the canary; if URL rewriting / path handling is wrong,
# `helm repo add` (which probes index.yaml) fails.
RUN helm repo add depsilo "${HELM_REPO_URL}" \
    && helm repo update \
    && helm search repo depsilo | head -5 \
    && echo "helm: index.yaml OK"
