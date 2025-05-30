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
