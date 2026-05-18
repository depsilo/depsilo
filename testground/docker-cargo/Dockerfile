FROM rust:1.75-slim

ARG CARGO_REGISTRY

RUN mkdir -p /root/.cargo && printf '%s\n' \
    '[source.crates-io]' \
    'replace-with = "depsilo"' \
    '[source.depsilo]' \
    "registry = \"sparse+${CARGO_REGISTRY}\"" \
    > /root/.cargo/config.toml

WORKDIR /test
RUN cargo init --name cargotest --quiet \
    && echo 'serde = "1.0"' >> Cargo.toml \
    && cargo fetch

RUN echo "cargo: serde OK"
