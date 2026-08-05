FROM rust:1.75-slim@sha256:70c2a016184099262fd7cee46f3d35fec3568c45c62f87e37f7f665f766b1f74

ARG DEPSILO_URL

RUN mkdir -p /root/.cargo && printf '%s\n' \
    '[source.crates-io]' \
    'replace-with = "depsilo"' \
    '[source.depsilo]' \
    "registry = \"sparse+${DEPSILO_URL}/crates/\"" \
    > /root/.cargo/config.toml

WORKDIR /test
RUN cargo init --name cargotest --quiet \
    && echo 'serde = "1.0"' >> Cargo.toml \
    && cargo fetch

RUN echo "cargo: serde OK"
