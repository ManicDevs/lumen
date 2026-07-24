# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in Lumen, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email: [security@manicdevs.com](mailto:security@manicdevs.com)

Include:

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and provide a timeline for a fix.

## Security Considerations

Lumen is designed with security in mind:

- **Zero external dependencies** — no supply chain attack surface
- **No cloud APIs** — all inference runs locally via Ollama
- **No secrets in code** — environment variables only
- **Path traversal protection** — sandboxed file access in agent mode
- **Input validation** — all config values validated at load time
- **No shell execution** — pure Go stdlib HTTP client, no `os/exec`

## Best Practices

When deploying Lumen:

1. Run with最小权限 (least privilege)
2. Use `.env.example` as a template, never commit `.env`
3. Keep Ollama server updated
4. Run in a sandboxed environment (Docker recommended)
