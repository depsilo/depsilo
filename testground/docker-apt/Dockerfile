FROM ubuntu:22.04

ARG APT_MIRROR

# Replace default sources with proxy if APT_MIRROR is set
RUN if [ -n "$APT_MIRROR" ]; then \
      sed -i "s|http://archive.ubuntu.com|${APT_MIRROR}|g" /etc/apt/sources.list && \
      sed -i "s|http://security.ubuntu.com|${APT_MIRROR}|g" /etc/apt/sources.list; \
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
