FROM continuumio/miniconda3@sha256:1808b31ef43e9c521cde5884ba4df9ec26d8d503a314cea787590f8550358a63

ARG DEPSILO_URL

# Tuna-style upstream layout: <root>/pkgs/main/<subdir>/repodata.json
#
# Two tricky bits:
#  1. conda defaults to fetching repodata.json.zst (zstd-compressed),
#     which the upstream may not provide → 404. Disable.
#  2. `conda config --add channels <URL>` has surprising path semantics
#     (a `pkgs/main` suffix gets dropped in subdir composition for some
#     conda versions). Use `-c <URL> --override-channels` on the install
#     command instead — it bypasses .condarc entirely.
RUN conda config --set repodata_use_zst false \
    && conda config --set ssl_verify false

RUN conda install -y --override-channels \
    -c "${DEPSILO_URL}/conda/pkgs/main" \
    -n base requests \
    && python -c "import requests; print('conda: requests', requests.__version__, 'OK')"
