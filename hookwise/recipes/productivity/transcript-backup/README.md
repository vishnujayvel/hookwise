# Transcript Backup

Automatically backs up session transcripts for later review.

## What it does

- Enables transcript backup
- Limits backup directory to 100 MB by default

## Usage

```yaml
includes:
  - "builtin:productivity/transcript-backup"
```

## Customization

```yaml
includes:
  - "builtin:productivity/transcript-backup"

transcript_backup:
  max_dir_size_mb: 500
```
