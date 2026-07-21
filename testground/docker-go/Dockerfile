FROM golang:1.21-alpine

ARG DEPSILO_URL

ENV GOPROXY=${DEPSILO_URL}/go
ENV GOSUMDB=off

WORKDIR /test
RUN go mod init testmod && go get golang.org/x/text@v0.14.0

RUN go list -m golang.org/x/text && echo "go: x/text OK"
