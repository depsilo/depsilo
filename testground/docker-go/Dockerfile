FROM golang:1.21-alpine@sha256:2414035b086e3c42b99654c8b26e6f5b1b1598080d65fd03c7f499552ff4dc94

ARG DEPSILO_URL

ENV GOPROXY=${DEPSILO_URL}/go
ENV GOSUMDB=off

WORKDIR /test
RUN go mod init testmod && go get golang.org/x/text@v0.14.0

RUN go list -m golang.org/x/text && echo "go: x/text OK"
