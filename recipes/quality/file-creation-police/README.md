# File Creation Police

Quality recipe that monitors file creation and warns about proliferation.

## Features

- Tracks number of new files created per session
- Warns when count exceeds threshold (default: 5)
- Detects similar-named files (strip-trailing-digits algorithm)
- Configurable ignore patterns for test files

## Configuration

```yaml
include:
  - recipes/quality/file-creation-police

# Override in hookwise.yaml:
# config.maxNewFiles: 10
# config.ignorePatterns: ["*.test.ts", "*.spec.ts"]
```
