FROM python:3.11-alpine

ARG HF_ENDPOINT
ENV HF_ENDPOINT=${HF_ENDPOINT}

RUN apk add --no-cache git
RUN pip install --no-cache-dir huggingface_hub

# Use a tiny published model so the E2E completes in seconds.
# prajjwal1/bert-tiny is ~17 MB — same protocol surface as a 50 GB model,
# vastly smaller download. If this model becomes unavailable, swap to
# any public model under ~50 MB.
RUN huggingface-cli download prajjwal1/bert-tiny --local-dir /tmp/m \
    && test -f /tmp/m/config.json \
    && python -c "import json; cfg = json.load(open('/tmp/m/config.json')); \
                  assert cfg.get('model_type') == 'bert', cfg; print('config.json OK')"

CMD ["echo", "depsilo huggingface E2E passed"]
