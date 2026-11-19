FROM alpine/helm:3.13.3@sha256:904fe876ba5eb93dea4c10ff12c44f19c81dd2ae81e61b60e50ea4863b4676c1

ARG DEPSILO_URL

# `helm repo add` probes index.yaml, so a broken index route fails here.
RUN helm repo add depsilo "${DEPSILO_URL}/helm" \
    && helm repo update \
    && helm search repo depsilo | head -5 \
    && echo "helm: index.yaml OK"

# Depsilo serves chart URLs on the Upstream's own origin as relative paths, so
# Helm resolves them against the repository URL above. Pull one such chart:
# the download goes through Depsilo, not to the Upstream directly.
COPY relative-chart.awk /relative-chart.awk
RUN set -eu; \
    index="$(helm env HELM_REPOSITORY_CACHE)/depsilo-index.yaml"; \
    chart="$(awk -f /relative-chart.awk "$index")"; \
    if [ -z "$chart" ]; then echo "helm: no relative chart URL in the served index" >&2; exit 1; fi; \
    set -- $chart; \
    mkdir -p /tmp/charts; \
    helm pull "depsilo/$1" --version "$2" -d /tmp/charts; \
    ls /tmp/charts/*.tgz; \
    echo "helm: chart $1 $2 pulled through Depsilo"
