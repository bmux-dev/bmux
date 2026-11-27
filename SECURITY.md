# Security Policy

## Supported Versions

Security fixes are applied to `main` and released on the latest `0.x` minor version. Users should run the latest published release via `bmux update` or `brew upgrade --cask bmux`.

## Reporting a Vulnerability

Please report suspected security vulnerabilities privately using [GitHub Security Advisories](https://github.com/bmux-dev/bmux/security/advisories/new) instead of opening a public issue.

When reporting an issue, include:

- A description of the vulnerability and its potential impact (such as local privilege escalation, untrusted `.bmux.toml` / hook execution, terminal escape sequence injection, or IPC socket access).
- Reproduction steps or a minimal proof of concept.
- Your `bmux --version`, OS, and terminal emulator.

We aim to acknowledge reports within 72 hours and coordinate a fix before public disclosure.
