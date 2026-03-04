# Coaching

Ambient coaching features that nudge you when patterns suggest you are drifting off track -- nudges, not interruptions.

## Builder's Trap Detection

Monitors your tool usage patterns and categorizes them as coding, prep, tooling, practice, or neutral. When you have been in "tooling mode" too long:

- **30 min** -- Yellow: "Is this moving the needle?"
- **60 min** -- Orange: "Time to refocus."
- **90 min** -- Red: "Stop and ask: what was my original goal?"

<div align="center">
<img src="../../screenshots/builder-trap.png" alt="Builder's Trap detection" width="600">
</div>

### Configuration

```yaml
coaching:
  builder_trap:
    enabled: true
    thresholds:
      yellow: 30
      orange: 60
      red: 90
```

## Metacognition Coaching

Periodic prompts injected via `additionalContext` on a configurable timer (default: every 5 minutes). Cycles through a prompt list to break autopilot mode:

- "What assumption am I making right now that I haven't verified?"
- "Am I solving the right problem, or the problem I want to solve?"
- "What's the simplest thing that could possibly work here?"
- "Is this complexity essential, or am I over-engineering?"

### Configuration

```yaml
coaching:
  metacognition:
    enabled: true
    interval_seconds: 300
```

## Communication Coach

Grammar and communication analysis for interview prep and professional writing.

---

← [Back to Home](/)
