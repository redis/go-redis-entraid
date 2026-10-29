# v1.1.0 (2026-10-29)

## Introduction

This release adds context-aware credential subscriptions and an explicit way to
shut down a credentials provider, and fixes several lifecycle issues in the
provider and token manager.

- **`SubscribeWithContext`**: the provider can bound a caller's wait for
  credentials with a context. Canceling the context ends only that caller's
  wait; a token request shared with other connections keeps running. go-redis
  will use this method once it supports context-aware credentials providers.
- **`Close`**: providers implement `entraid.CredentialsProviderCloser`. The
  application owns the provider and closes it after its clients, including when
  Redis dialing failed before any subscription. Closing one Redis client does not
  close a shared provider. After `Close`, new subscriptions fail with
  `entraid.ErrCredentialsProviderClosed`.

```go
if c, ok := provider.(entraid.CredentialsProviderCloser); ok {
    defer c.Close()
}
```

See [Provider ownership and shutdown](README.md#provider-ownership-and-shutdown)
and [examples/entraid/provider_lifecycle](examples/entraid/provider_lifecycle).

## Changes

## 🚀 Features

- feat(provider): support context-aware credential subscriptions ([#22](https://github.com/redis/go-redis-entraid/pull/22))
- feat(provider): add explicit owner-controlled shutdown ([#24](https://github.com/redis/go-redis-entraid/pull/24))

## 🐛 Bug Fixes

- A failed token manager restart no longer blocks later subscriptions ([#18](https://github.com/redis/go-redis-entraid/issues/18)) ([#22](https://github.com/redis/go-redis-entraid/pull/22))
- A subscription racing the last unsubscribe no longer succeeds without token renewal ([#21](https://github.com/redis/go-redis-entraid/issues/21)) ([#22](https://github.com/redis/go-redis-entraid/pull/22))
- Transient Azure credential timeouts are retried instead of stopping renewal ([#19](https://github.com/redis/go-redis-entraid/issues/19)) ([#23](https://github.com/redis/go-redis-entraid/pull/23))
- Credentials providers can be stopped after Redis dialing fails ([#20](https://github.com/redis/go-redis-entraid/issues/20)) ([#24](https://github.com/redis/go-redis-entraid/pull/24))

## 🧰 Maintenance

- fix(ci): fix action version ([#17](https://github.com/redis/go-redis-entraid/pull/17))
- fix(ci): bump golangci-lint, fix expired test JWT ([#25](https://github.com/redis/go-redis-entraid/pull/25))
- test(manager): fix flaky start/stop test, drop lock-only benchmarks ([#26](https://github.com/redis/go-redis-entraid/pull/26))

## Contributors

Thanks to @PragalvaXFREZ for #22, #23 and #24, and to @terrorobe for reporting
#18, #19, #20 and #21.

## Compatibility

- Go: 1.23+
- go-redis: v9.9.0+

# v1.0.3 (2025-05-30)

## Introduction

ClientID in CredentialsProviderOptions is not used and will be removed in a future version.
The correct one to use is the one in the identity provider options (e.g. ConfidentialIdentityProviderOptions).

## Changes

## 🧰 Maintenance

- refactor(provider): Mark ClientID as deprecated, use correct one in examples. ([#8](https://github.com/redis/go-redis-entraid/pull/8))

## Compatibility

- Go: 1.23+
- go-redis: v9.9.0+

# v1.0.2 (2025-05-29)

## Changes

- fix(manager): optimize durationToRenewal ([#6](https://github.com/redis/go-redis-entraid/pull/6))
- chore(documentation): add release notes, add badges in readme ([#7](https://github.com/redis/go-redis-entraid/pull/7))
- chore(dependencies): update dependencies

## Compatibility

- Go: 1.23+
- go-redis: v9.9.0+

# v1.0.0 (2025-05-27)

## Introduction

We are excited to announce the General Availability release of **go-redis-entraid**, a Go library that enables seamless Entra ID (formerly Azure AD) authentication for Redis Enterprise Cloud.

## Background

Redis Enterprise Cloud supports Microsoft Entra ID for authentication, allowing you to use your organization's existing identity management system to control access to Redis databases. The go-redis-entraid library bridges the gap between the popular [go-redis](https://github.com/redis/go-redis) client and Entra ID, providing:

- Automatic token acquisition and renewal
- Support for multiple authentication mechanisms
- Seamless integration with existing Redis applications
- Secure credential management

## Key Features

- **Multiple Authentication Methods**: Support for various Entra ID authentication flows:
  - Client Secret
  - Client Certificate
  - Managed Identity (System and User-assigned)
  - Default Azure Identity Provider (for local development)

- **Automatic Token Management**: Handles token acquisition, caching, and renewal without requiring manual intervention.

- **Configuration Flexibility**: Supports configuration through environment variables, code, or configuration files.

- **Comprehensive Error Handling**: Detailed error information and recovery strategies.

## Getting Started

### Installation

```bash
go get github.com/redis/go-redis-entraid@v1.0.0
```

## Compatibility

- Go: 1.16+
- go-redis: v9.9.0+
