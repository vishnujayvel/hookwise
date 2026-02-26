---
layout: home

hero:
  name: Hookwise
  text: Smart hooks framework for Claude Code
  tagline: Guards, analytics, coaching, feeds, and an interactive TUI -- all config-driven.
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: CLI Reference
      link: /reference/cli-reference

features:
  - title: Guards
    details: Block, warn, or confirm tool calls with declarative YAML rules. First-match-wins evaluation with picomatch patterns.
  - title: Analytics
    details: Track sessions, tool calls, file edits, and AI authorship in a local SQLite database. View stats from the CLI or TUI.
  - title: Coaching
    details: Metacognition prompts, builder's trap detection, and communication coaching to keep you focused and productive.
  - title: Feed Platform
    details: Background daemon with 5 built-in producers (pulse, project, calendar, news, insights) and a TTL-aware cache bus.
  - title: Status Line
    details: Two-tier status line with 19 segments. Renders in your Claude Code prompt with live data from the cache bus.
  - title: Recipes
    details: Pre-built, shareable hook configurations. Include community recipes or create your own with guards, handlers, and config.
---
