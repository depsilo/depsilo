FROM golang:1.27.4-alpine3.23@sha256:146e3b9c7c1e2ec3d3f5348c8a686151a75e1976e50efac9fc022ebe446a9a82

ARG DEPSILO_URL

ENV GOPROXY=${DEPSILO_URL}/go
ENV GOSUMDB=off

WORKDIR /test
RUN go mod init testmod && go get golang.org/x/text@v0.14.0

RUN go list -m golang.org/x/text && echo "go: x/text OK"
