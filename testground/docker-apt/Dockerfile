FROM ubuntu:22.04@sha256:0e0a0fc6d18feda9db1590da249ac93e8d5abfea8f4c3c0c849ce512b5ef8982

ARG DEPSILO_URL

# Replace default sources with the Depsilo APT route when configured.
RUN if [ -n "$DEPSILO_URL" ]; then \
      sed -i "s|http://archive.ubuntu.com|${DEPSILO_URL}/apt|g" /etc/apt/sources.list && \
      sed -i "s|http://security.ubuntu.com|${DEPSILO_URL}/apt|g" /etc/apt/sources.list; \
    fi

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    wget \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Verify installation
RUN curl --version | head -1 && echo "curl OK"
RUN wget --version | head -1 && echo "wget OK"
RUN jq --version && echo "jq OK"
