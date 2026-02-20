# Secret Scanning

Safety recipe that warns when Write/Edit tool operations may expose secrets.

## What It Detects

- **Sensitive files:** .env, credentials.json, .pem, .key, id_rsa, etc.
- **API key patterns:** AWS (AKIA...), OpenAI/Anthropic (sk-...), GitHub (ghp_...), GitLab (glpat-...)

## Configuration

```yaml
include:
  - recipes/safety/secret-scanning

# Override patterns in hookwise.yaml:
# config.sensitiveFilePatterns: [".env", "secrets.yaml"]
# config.apiKeyPatterns: ["my-custom-key-[a-z]{20}"]
```
