# Transcript Backup

Productivity recipe that archives PreCompact events for transcript preservation.

## How It Works

- Listens for PreCompact events (before context window compaction)
- Saves payloads as timestamped JSON files
- Enforces maximum backup directory size

## Configuration

```yaml
include:
  - recipes/productivity/transcript-backup

# Override in hookwise.yaml:
# config.backupDir: /path/to/backups
# config.maxSizeMb: 200
```
