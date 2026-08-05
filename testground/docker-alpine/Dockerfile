FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

ARG DEPSILO_URL

# Replace both repositories instead of adding a supplemental -X source. This
# proves the index and package downloads all pass through Depsilo.
RUN release="v$(cut -d. -f1,2 /etc/alpine-release)" \
    && printf '%s\n' \
      "${DEPSILO_URL}/alpine/${release}/main" \
      "${DEPSILO_URL}/alpine/${release}/community" \
      > /etc/apk/repositories \
    && apk add --no-cache curl

RUN curl --version | head -1 && echo "alpine: curl OK"
