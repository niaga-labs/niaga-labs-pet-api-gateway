# Changelog

All notable changes to the Kilat Pet Delivery API gateway -- the single entry point that checks JWTs and routes `/api/v1/*` to the services behind it are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Go module path is now `github.com/niaga-labs/niaga-labs-pet-api-gateway` (was
  `github.com/Kilat-Pet-Delivery/api-gateway`), and every import of lib-common and lib-proto follows. The repos moved to the niaga-labs org on
  2026-09-12 (HQ-40). The `replace => ../lib-*` targets are unchanged: local folders keep their
  short names. (HQ-41)
