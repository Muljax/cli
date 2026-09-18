<div align="center">

# Contributing to Muljax CLI

Thanks for contributing to the Muljax CLI!

The official command-line interface for the [Muljax Identity Platform](https://github.com/Muljax/id).

<br />

[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://golang.org/)
[![OpenSSH](https://img.shields.io/badge/OpenSSH-231F20?logo=gnubash&logoColor=white)](https://www.openssh.com/)
[![OAuth 2.0](https://img.shields.io/badge/OAuth%202.0-EB5424?logo=auth0&logoColor=white)](https://oauth.net/2/)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](https://www.kernel.org/)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](https://apple.com/)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](https://microsoft.com/)

</div>

## Getting Started

Muljax CLI is written in Go.

For installation, client onboarding, and server integration instructions, see the [Setup Guide](./SETUP.md).

## Repository Structure

```text
.
├── cmd/
│   ├── root.go             # Root Cobra command and global flags
│   ├── id/                 # Identity and auth commands (muljax id auth)
│   │   └── id.go
│   └── ssh/                # SSH CA commands (muljax ssh)
│       └── ssh.go
├── pkg/
│   ├── auth/               # OAuth 2.0 PKCE implementation & token exchange
│   │   ├── oauth.go
│   │   ├── pkce.go
│   │   └── pkce_test.go
│   ├── client/             # Muljax ID API & SSH CA client
│   │   ├── client.go
│   │   └── client_test.go
│   ├── config/             # CLI persistent configuration
│   │   └── config.go
│   ├── sshutil/            # Key generation, cert parsing, ~/.ssh/config modification
│   │   ├── cert.go
│   │   ├── config.go
│   │   ├── keys.go
│   │   └── sshutil_test.go
│   ├── storage/            # OS keyring and local storage
│   │   └── token.go
│   └── ui/                 # CLI output formatting, colors, badges
│       └── ui.go
├── CONTRIBUTING.md
├── go.mod
├── go.sum
├── LICENSE
├── main.go                 # Entrypoint
├── README.md
├── SECURITY.md
└── SETUP.md
```

## Development

### Prerequisites

- [Go](https://golang.org/) 1.24 or higher
- `git`

### Building

To compile the binary locally:

```bash
go build -o muljax .
```

To test the binary:

```bash
./muljax --help
```

### Running Tests

Execute all tests across all packages:

```bash
go test -v ./...
```

To run tests with race detection enabled:

```bash
go test -race ./...
```

## Code Quality

Before submitting changes, ensure your code passes standard Go formatting and static analysis:

```bash
# Format code
go fmt ./...

# Run static analysis
go vet ./...

# Run all unit tests
go test ./...
```

Please do not submit pull requests that fail tests or introduce compilation or formatting issues.

## Making Changes

1. Create a branch for your work:
   ```bash
   git checkout -b feature/my-change
   # or: git checkout -b fix/issue-description
   ```
2. Implement your changes and add corresponding unit tests where applicable (e.g. under `pkg/*/`).
3. Run tests and static analysis:
   ```bash
   go test ./...
   go vet ./...
   ```
4. Review your diff:
   ```bash
   git diff
   ```
5. Commit your changes following Conventional Commits format:
   ```bash
   git add .
   git commit -m "feat(ssh): add custom renewal threshold flag"
   ```

## Commit Messages

Muljax uses [Conventional Commits](https://www.conventionalcommits.org/) for git commit messages.

Format:

```text
<type>(<optional scope>): <description>
```

Common types include:

- `feat` — new functionality or command
- `fix` — bug fix
- `refactor` — code restructuring without altering external behavior
- `docs` — documentation additions or updates
- `test` — adding or improving test coverage
- `chore` — maintenance, dependencies, or tooling

Examples:

```text
feat(ssh): support custom certificate principal selection
fix(auth): handle missing browser environment on headless linux
docs: update OpenSSH Match exec configuration instructions
test(client): add mock test for ca revocation parsing
```
