# Changelog

All notable changes to go-redis-entraid will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2025-05-27

### Added
- Initial General Availability release
- Multiple authentication methods:
  - Client Secret authentication
  - Client Certificate authentication
  - Managed Identity (System and User-assigned)
  - Default Azure Identity Provider for local development
- Automatic token acquisition and renewal
- Configurable token refresh policies
- Thread-safe token management
- Comprehensive error handling and recovery strategies
- Configuration support via environment variables, code, or configuration files

### Compatibility
- Go: 1.16+
- go-redis: v9.10.0+

[1.0.0]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.0
