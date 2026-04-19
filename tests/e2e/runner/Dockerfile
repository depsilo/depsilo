FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Install base tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip \
    nodejs npm \
    golang-go \
    curl wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Go needs a writable GOPATH
ENV GOPATH=/tmp/gopath
ENV PATH=$GOPATH/bin:$PATH

WORKDIR /tests
COPY run-tests.sh /tests/run-tests.sh
RUN chmod +x /tests/run-tests.sh

ENTRYPOINT ["/tests/run-tests.sh"]
