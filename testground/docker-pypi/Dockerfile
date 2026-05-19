FROM python:3.11-slim

ARG PIP_INDEX_URL
ARG PIP_TRUSTED_HOST

RUN pip install --no-cache-dir \
    --index-url "${PIP_INDEX_URL}" \
    --trusted-host "${PIP_TRUSTED_HOST}" \
    requests

RUN python -c "import requests; print('pypi:', 'requests', requests.__version__, 'OK')"
