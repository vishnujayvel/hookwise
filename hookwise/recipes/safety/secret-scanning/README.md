# Secret Scanning

Warns or requires confirmation when accessing files that may contain secrets.

## What it does

- **Warns** when reading `.env` files or files with "credentials" in the path
- **Confirms** before writing to `.env` files

## Usage

```yaml
includes:
  - "builtin:safety/secret-scanning"
```
