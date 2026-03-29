# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-03-29

### Added

- PyPI package proxy with smart URL rewriting and caching
- APT repository proxy with GPG signature passthrough
- Multi-upstream support with priority-based selection
- Per-upstream HTTP proxy configuration
- Automatic health checking with latency tracking
- Local filesystem and S3-compatible storage backends
- SQLite and PostgreSQL database support
- Singleflight deduplication to prevent cache stampede
- Streaming response — no full buffering in memory
- LRU cache eviction with configurable threshold
- Circuit breaker and rate limiting per upstream
- Web portal with Quick Start guide and service status
- Admin dashboard with cache management, upstream control, access logs, user management
- JWT authentication for admin API
- API token support with scoped permissions
- Prometheus metrics endpoint (`/metrics`)
- Docker and docker-compose deployment support
- Configurable via TOML file and environment variables
