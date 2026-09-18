# Security Policy

Muljax takes security seriously. If you believe you have found a security vulnerability in the Muljax CLI, please report it responsibly through GitHub's private vulnerability reporting.

## Supported Versions

Muljax CLI is currently pre-1.0. Only the **latest released version** is supported with security updates.

| Version                | Supported          |
| ---------------------- | ------------------ |
| Latest pre-1.0 release | :white_check_mark: |
| Older releases         | :x:                |

## Cryptographic Security Boundaries

The Muljax CLI is designed around zero-trust and defense-in-depth principles:
- **Private Keys**: Asymmetric Ed25519 private keys are generated locally using cryptographically secure random sources (`crypto/rand`) and stored with `0600` permissions. The private key never leaves your workstation.
- **Certificate Authority Signing**: Only public keys are submitted to the Muljax CA for signing.
- **Credential Storage**: OAuth tokens are saved to the OS-native keychain via system APIs. On systems lacking a keychain service, permissions on the fallback token file are locked to `0600`.
- **PKCE**: Browser authentication enforces OAuth 2.0 PKCE (RFC 7636) with SHA-256 code challenges on loopback sockets.

## Reporting a Vulnerability

Please report security vulnerabilities using **GitHub's private vulnerability reporting** for this repository.

Do not disclose security vulnerabilities through public GitHub issues, discussions, or pull requests.

When reporting a vulnerability, please provide as much information as possible, including:

- A clear description of the vulnerability
- Exact steps to reproduce the issue
- The potential security impact
- Any relevant logs, terminal output, or proof-of-concept
- The affected operating system, architecture, and CLI version

We will review submitted reports and respond as soon as reasonably possible.

If the vulnerability is confirmed, we will work to address it and coordinate public disclosure as appropriate. If a report is determined not to be a security vulnerability, we will provide an explanation.

> [!WARNING]
> Please do not include secrets, credentials, SSH private keys, OAuth tokens, or other sensitive information in a vulnerability report unless it is strictly necessary to demonstrate the vulnerability.
