# Changelog

All notable changes to go-redis-entraid will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-10-29

### Added
- feat(provider): support context-aware credential subscriptions (`SubscribeWithContext`). Canceling the context ends only that caller's wait; a token request shared with other connections keeps running. @PragalvaXFREZ (#22)
- feat(provider): add explicit owner-controlled shutdown. Providers implement `CredentialsProviderCloser`; `Close` stops the token manager, also when Redis dialing failed before any subscription. Adds `ErrCredentialsProviderClosed` and the optional `manager.TokenManagerStopper`. @PragalvaXFREZ (#24)

### Fixed
- fix(provider): a failed token manager restart no longer blocks later subscriptions (#18) @PragalvaXFREZ (#22)
- fix(provider): a subscription racing the last unsubscribe no longer succeeds without token renewal (#21) @PragalvaXFREZ (#22)
- fix(manager): retry transient Azure credential timeouts instead of stopping renewal (#19) @PragalvaXFREZ (#23)
- fix(provider): credentials providers can be stopped after Redis dialing fails (#20) @PragalvaXFREZ (#24)

### Changed
- fix(ci): fix action version @ndyakov (#17)
- fix(ci): bump golangci-lint, fix expired test JWT @ndyakov (#25)
- test(manager): fix flaky start/stop test, drop lock-only benchmarks @ndyakov (#26)

### Compatibility
- Go: 1.23+
- go-redis: v9.9.0+

## [1.0.7] - 2026-01-14

### Fixed
- fix(tenant): fix swapped `AuthorityTypeDefault` and `AuthorityTypeMultiTenant` implementations @ndyakov (#16)

### Changed
- tests(integration): add example tests instead of applications @ndyakov (#14)

## [1.0.6] - 2025-09-12

### Changed
- chore: update changelog @ndyakov (#13)

## [1.0.5] - 2025-09-11

### Fixed
- fix: don't hold lock when calling listeners @ndyakov (#12)

## [1.0.4] - 2025-08-06

## Changed
- refactor(manager): small refactors around the manager and token logic @ndyakov (#10)

## [1.0.3] - 2025-05-30

### Changed
- refactor(provider): Mark ClientID as deprecated, use correct one in examples. (#8)

## [1.0.2] - 2025-05-29

### Changed
- chore(documentation): add release notes, add badges in readme @ndyakov (#7)
- fix(manager): optimize durationToRenewal @ndyakov (#6)

## [1.0.1] - 2025-05-27

### Changed
- chore(deps): update dependencies @ndyakov (#5)
- refactor(github): move templates, add changelog @ndyakov (#4)

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
[1.0.1]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.1
[1.0.2]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.2
[1.0.3]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.3
[1.0.4]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.4
[1.0.5]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.5
[1.0.6]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.6
[1.0.7]: https://github.com/redis/go-redis-entraid/releases/tag/v1.0.7
[1.1.0]: https://github.com/redis/go-redis-entraid/releases/tag/v1.1.0
