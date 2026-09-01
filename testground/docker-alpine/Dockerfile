FROM alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

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
