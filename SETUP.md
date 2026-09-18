<div align="center">
	<img width="170" alt="Muljax Logo" src="https://github.com/user-attachments/assets/e9d50a92-6993-48e7-871e-d3b497cf721f" />
</div>
<br />

---

# Muljax CLI Setup & Integration Guide

Comprehensive guide for configuring the Muljax CLI, client workstations, and target OpenSSH servers.

[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://golang.org/)
[![OpenSSH](https://img.shields.io/badge/OpenSSH-231F20?logo=gnubash&logoColor=white)](https://www.openssh.com/)
[![OAuth 2.0](https://img.shields.io/badge/OAuth%202.0-EB5424?logo=auth0&logoColor=white)](https://oauth.net/2/)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](https://www.kernel.org/)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](https://apple.com/)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](https://microsoft.com/)

</div>

---

## Prerequisites

Before setting up `muljax`, ensure you have:

- A working installation of [Go](https://golang.org/) 1.24+ (if building from source)
- An [OpenSSH](https://www.openssh.com/) client (version 7.3 or higher with `Match exec` support)
- A running [Muljax Identity Platform](https://github.com/Muljax/id) instance (with the SSH Certificate Authority module enabled)
- Network access to your Muljax ID endpoint (e.g., `https://id.example.com` or `http://localhost:8787` for local development)

---

## Part 1: Client Workstation Setup

### 1. Build and Install Binary

From the repository root:

```bash
go build -o muljax .
./muljax install
```

*(You can also run `./muljax install --user` to install to `~/.local/bin` without sudo)*

Verify the binary is available in your shell:

```bash
muljax --help
```

### 2. Run Automated Onboarding

Run the onboarding wizard to configure your local keys, authenticate, obtain your initial certificate, and register the OpenSSH client hook:

```bash
muljax ssh setup
```

When run interactively in a terminal, the wizard guides you through the setup:
- **Target Endpoint**: Asks for your Muljax ID API endpoint URL (e.g. `https://id.your-domain.com`), defaulting to your existing configuration or `http://localhost:8787`.
- **Host Scoping**: Asks for your target host pattern (e.g. `*.your-domain.com,*.internal`) to prevent OpenSSH from activating `muljax` on public Git hosts or personal servers.

*(Tip: For non-interactive scripts, pass flags directly: `muljax ssh setup --endpoint https://id.example.com --hosts "*.corp.example.com,*.internal"`)*

This command executes the following sequence:

1. **Key Generation**: Creates a local Ed25519 asymmetric key pair at `~/.ssh/muljax_id_ed25519` (permissions `0600`) and `~/.ssh/muljax_id_ed25519.pub` (`0644`). If keys already exist, it parses and reuses them.
2. **OAuth 2.0 Authentication**: Binds an ephemeral local web server on `127.0.0.1`, opens your default browser to `/oauth/authorize` with PKCE, and exchanges the authorization code for an access token and refresh token.
3. **Secure Credential Storage**: Persists the session tokens into your operating system's native keychain (macOS Keychain, Linux Secret Service / DBus, Windows Credential Manager) with a file-based fallback (`~/.config/muljax/tokens.json`).
4. **Certificate Issuance**: Submits the public key to `/ssh/certs/issue` and saves the signed OpenSSH user certificate to `~/.ssh/muljax_id_ed25519-cert.pub` along with metadata (`.meta.json`).
5. **OpenSSH Configuration**: Safely prepends a scoped automated renewal hook to `~/.ssh/config`.

### 3. OpenSSH Client Configuration & Scoping

The `muljax ssh setup` command injects an idempotent configuration block at the top of your `~/.ssh/config`:

```sshconfig
# BEGIN MULJAX SSH CONFIG
Match host *.corp.example.com,*.internal exec "muljax ssh ensure-cert --quiet"
    IdentityFile ~/.ssh/muljax_id_ed25519
# END MULJAX SSH CONFIG
```

#### Why Scoping Matters
Using a scoped pattern (e.g. `*.corp.example.com,*.internal` or private IP ranges like `10.*`) instead of a catch-all wildcard (`Match host *`) is best practice:
- **Prevents Unnecessary Overhead**: Connections to public Git hosts (`github.com`, `gitlab.com`) and personal servers bypass `muljax` completely.
- **Prevents Identity Leakage**: Your corporate certificate (including your Key ID email and principals) is never advertised to untrusted third-party SSH servers.
- **Avoids `MaxAuthTries` Exhaustion**: Prevents consuming authentication attempts on servers that don't know your Muljax key.
- **Seamless Offline Operation**: SSH connections to local devices or personal machines work smoothly even when offline or disconnected from VPN.

#### How the Hook Functions
- When you execute `ssh user@internal-server`, OpenSSH matches the host pattern and executes `muljax ssh ensure-cert --quiet`.
- **Fast Path (<3ms)**: If the existing certificate is valid and has more than 30 minutes of validity remaining, `muljax` exits immediately with code `0`. OpenSSH completes the connection without delay.
- **Auto-Renewal**: If the certificate is missing, expired, or nearing expiration (<30 minutes remaining), `muljax` uses your stored OAuth 2.0 refresh token to fetch a fresh certificate in the background before OpenSSH begins the cryptographic handshake.
- Because `IdentityFile` points to `~/.ssh/muljax_id_ed25519`, OpenSSH automatically looks for `~/.ssh/muljax_id_ed25519-cert.pub` to present alongside the private key.

### 4. Verifying Client Status

You can check the health of your SSH credentials at any time:

```bash
# View key paths, certificate serial, principals, validity, and live revocation status
muljax ssh status

# View raw certificate details
muljax ssh cert

# Check authentication token status
muljax id auth status
```

### 5. Disabling or Resetting OpenSSH Integration

If you need to remove the Muljax automated hook from your `~/.ssh/config`:

```bash
muljax ssh unhook
```

This will safely and cleanly excise the `# BEGIN MULJAX SSH CONFIG` block without altering any other host configurations in `~/.ssh/config`.

---

## Part 2: Target Server Setup

Target servers must be configured to trust the Muljax Certificate Authority and authorize certificate principals.

### 1. Retrieve the CA Public Key

Run the following command on your workstation (or in your deployment automation):

```bash
muljax ssh server setup
```

This retrieves the active CA public key directly from your Muljax ID endpoint (via `/ssh/ca/public-key?format=raw`).

### 2. Install CA Public Key on the Server

On the target Linux/Unix server, write the CA public key to `/etc/ssh/muljax_ca.pub`:

```bash
sudo mkdir -p /etc/ssh
sudo nano /etc/ssh/muljax_ca.pub
# Paste the single-line public key (e.g., ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... muljax-ssh-ca)
sudo chmod 644 /etc/ssh/muljax_ca.pub
```

### 3. Configure OpenSSH Daemon

Edit `/etc/ssh/sshd_config` (or place a file in `/etc/ssh/sshd_config.d/muljax.conf` if supported):

```sshconfig
# Trust user certificates signed by Muljax CA
TrustedUserCAKeys /etc/ssh/muljax_ca.pub

# Map authorized certificate principals to local accounts
AuthorizedPrincipalsFile /etc/ssh/auth_principals/%u
```

### 4. Define Authorized Principals

Create an authorized principals file for each system user account. The filename must match the local username (`%u`):

```bash
sudo mkdir -p /etc/ssh/auth_principals
sudo chmod 755 /etc/ssh/auth_principals
```

For a local user account named `ubuntu`:

```bash
sudo nano /etc/ssh/auth_principals/ubuntu
```

Add one principal per line that is permitted to log in as `ubuntu`:

```text
admin
ops
ubuntu
```

Secure the permissions:

```bash
sudo chmod 644 /etc/ssh/auth_principals/*
```

### 5. Validate and Reload SSH Daemon

Test the OpenSSH daemon configuration for syntax errors before reloading:

```bash
sudo sshd -t
```

Reload `sshd`:

```bash
sudo systemctl reload sshd
# On Alpine / non-systemd systems:
# sudo rc-service sshd reload
```

---

## Part 3: Certificate Lifecycle & Revocation

### Expiration & Auto-Renewal Threshold

- **Certificate Validity**: By default, issued certificates are valid for 8 hours (`28,800` seconds).
- **Renewal Threshold**: When `ensure-cert` runs, any certificate with less than **30 minutes** of validity remaining triggers an automatic renewal.
- **OAuth Session**: If the OAuth 2.0 access token has expired, `muljax` automatically exchanges the refresh token at `/oauth/token` without user interaction.
- **Session Re-authentication**: If the refresh token has expired or has been revoked on the server, `ensure-cert` will print an advisory message prompting you to re-authenticate:
  ```bash
  muljax ssh login
  ```

### Manual Renewal

To immediately force a new certificate (for example, with a 12-hour duration):

```bash
muljax ssh cert --renew --ttl 12
```

### Revocation Verification

The Muljax CA publishes a Key Revocation List (KRL) at `/ssh/ca/revoked-keys?format=raw`.

- `muljax` caches revocation query results for 5 minutes (`300` seconds) in `~/.ssh/muljax_id_ed25519-cert.pub.meta.json` to prevent network latency on every SSH connection.
- To bypass the cache and perform an immediate live revocation check:
  ```bash
  muljax ssh ensure-cert --force-check
  # or inspect the revocation status in:
  muljax ssh status
  ```
- If a certificate serial number appears on the revocation list, `muljax` automatically marks it revoked and attempts to issue a replacement certificate.

---

## Part 4: Headless & CI/CD Environments

In headless environments (Docker containers, CI/CD runners, bastion hosts) where no graphical browser or D-Bus / Secret Service daemon is present:

1. **Keyring Fallback**: If `go-keyring` cannot connect to a system keychain daemon, `muljax` automatically persists tokens to `~/.config/muljax/tokens.json` with strict `0600` permissions.
2. **Terminal Browser Fallback**: If the browser cannot be opened automatically via `xdg-open` / `open`, `muljax` prints the authorization URL to `stdout`:
   ```text
   ℹ Opening browser for Muljax authentication...
     If your browser did not open automatically, visit:
     https://id.example.com/oauth/authorize?client_id=muljax-cli...
   ```
   You can open this URL on any workstation and complete the login.
3. **Automated Pipeline Authentication**: You can pre-seed `~/.config/muljax/tokens.json` with a service account token or configure SSH keys directly.

---

## Part 5: Troubleshooting

### OpenSSH Debug Output

To diagnose SSH connection or certificate validation issues, run OpenSSH with verbose logging:

```bash
ssh -vvv user@server.internal
```

Look for lines indicating:
- `debug1: Executing Match exec "muljax ssh ensure-cert --quiet"`
- `debug1: Will attempt key: ~/.ssh/muljax_id_ed25519 ED25519-CERT`
- `debug1: Server accepts key: ~/.ssh/muljax_id_ed25519 ED25519-CERT`

### Verifying Certificate on the Command Line

Inspect your certificate directly with OpenSSH's native key parser:

```bash
ssh-keygen -L -f ~/.ssh/muljax_id_ed25519-cert.pub
```

Expected output:
```text
~/.ssh/muljax_id_ed25519-cert.pub:
        Type: ssh-ed25519-cert-v01@openssh.com user certificate
        Public key: ED25519 ...
        Signing CA: ED25519 ...
        Key ID: "user@example.com"
        Serial: 1048576
        Valid: from 2026-09-17T12:00:00 to 2026-09-17T20:00:00
        Principals:
                admin
                ubuntu
        Critical Options: (none)
        Extensions:
                permit-X11-forwarding
                permit-agent-forwarding
                permit-port-forwarding
                permit-pty
                permit-user-rc
```

### Common Issues

| Issue | Cause | Resolution |
| --- | --- | --- |
| `Permission denied (publickey)` | Principals mismatch or server missing CA key | Check `/etc/ssh/auth_principals/%u` on target server; verify `muljax ssh cert` principals match. |
| `certificate auto-renewal failed` | Refresh token expired or revoked | Run `muljax ssh login` or `muljax id auth login` to establish a new session. |
| `Match exec` hook failing | Binary not in system `PATH` | Ensure `muljax` is installed to `/usr/local/bin/muljax` or specify full binary path in `~/.ssh/config`. |
| Port binding error during login | Port conflict on loopback | The CLI binds to random loopback port `:0`; ensure local firewall does not block loopback sockets. |
