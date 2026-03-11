# Security Policy

## Supported Versions

MuxAgent CLI is still evolving quickly. Security fixes are only guaranteed for:

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| `main` branch | Best effort |
| Older releases | No |

If you are reporting a security issue, please reproduce it on the latest release
first when possible.

## Reporting a Vulnerability

Please do not open a public GitHub issue for suspected security vulnerabilities.

Preferred path:

1. Use GitHub's private vulnerability reporting for this repository.
2. If private reporting is not available, contact the maintainers on GitHub and
   ask for a private channel before sharing details.

Please include:

- A short description of the issue and why it matters.
- The affected version, commit, or release tag.
- Your OS and architecture.
- Clear reproduction steps or a minimal proof of concept.
- Any logs or screenshots that help explain the issue.

Please redact:

- Tokens, credentials, and QR payloads.
- The contents of `~/.muxagent/`.
- Private repository names, worktree paths, or personal machine details unless
  they are required to reproduce the issue.

## What Happens Next

The maintainers will try to:

- Acknowledge receipt within 5 business days.
- Confirm severity and scope as quickly as possible.
- Coordinate a fix and release before public disclosure when practical.

Please avoid publishing full details until a fix or mitigation is available.

## Scope

Security reports are especially useful for issues involving:

- Authentication and pairing flows.
- Relay communication and credential handling.
- Local daemon control endpoints.
- Runtime download, update, and release verification paths.
- Local storage of secrets or machine identity material.
