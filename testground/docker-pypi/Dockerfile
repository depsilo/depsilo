FROM python:3.11-slim

ARG DEPSILO_URL

RUN trusted_host="${DEPSILO_URL#*://}" \
    && trusted_host="${trusted_host%%:*}" \
    && pip install --no-cache-dir \
      --index-url "${DEPSILO_URL}/pypi/simple/" \
      --trusted-host "${trusted_host}" \
      requests

RUN python -c "import requests; print('pypi:', 'requests', requests.__version__, 'OK')"
