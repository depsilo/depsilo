FROM golang:1.21-alpine

ARG GOPROXY

ENV GOPROXY=${GOPROXY},direct
ENV GOSUMDB=off
ENV GOFLAGS=-insecure

WORKDIR /test
RUN go mod init testmod && go get golang.org/x/text@v0.14.0

RUN go list -m golang.org/x/text && echo "go: x/text OK"
