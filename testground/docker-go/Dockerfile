FROM golang:1.27.4-alpine3.23@sha256:146e3b9c7c1e2ec3d3f5348c8a686151a75e1976e50efac9fc022ebe446a9a82

ARG DEPSILO_URL

ENV GOPROXY=${DEPSILO_URL}/go

# The go command verifies module checksums against the checksum database:
# through Depsilo when a Go upstream mirrors it, directly otherwise. Fail if
# anything in the image turns that verification off.
RUN if [ "$(go env GOSUMDB)" = off ] || [ "$(go env GONOSUMDB)$(go env GOPRIVATE)$(go env GOINSECURE)" != "" ] || \
       go env GOFLAGS | grep -q -e -insecure; then \
        echo "go: checksum database verification is disabled" >&2; exit 1; \
    fi

WORKDIR /test
RUN go mod init testmod && go get golang.org/x/text@v0.14.0

RUN go list -m golang.org/x/text && echo "go: x/text OK"

# A verified lookup leaves the signed tree head it was checked against.
RUN database="$(go env GOSUMDB)" && \
    test -s "$(go env GOPATH)/pkg/sumdb/${database%% *}/latest" && \
    grep -q '^golang.org/x/text v0.14.0 h1:' go.sum && \
    go mod verify && echo "go: checksum database verification OK"
