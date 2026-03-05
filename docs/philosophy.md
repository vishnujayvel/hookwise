# Philosophy

hookwise is a platform for guarding, coaching, and observing Claude Code sessions.

## Why hookwise Exists

Claude Code is extraordinary. It can refactor an entire module, set up infrastructure, write tests -- all while you are reading the previous diff. That power is exactly why it needs guard rails, observability, and coaching.

The existing hooks system is powerful but raw. You write bash scripts, scatter them across your config, and hope they cover the right events. There is no testing, no sharing, no analytics, and no way to know if your guards actually work until something slips through.

hookwise replaces all of that with a platform: declarative guards, analytics that tell you where your time actually goes, coaching that nudges you when you have been in tooling mode for 90 minutes, feeds that surface context in your status line, recipes that let you share configurations, and a TUI that ties it all together.

## Core Principles

### 1. Platform, Not Just Config

hookwise started as a YAML config layer for guard rails. It is now a platform with a SQLite analytics engine, a cache bus, coaching state, a feed daemon, a TUI, and a recipe system. YAML remains the right format for declaring guards, coaching rules, and feed settings -- the things that should be human-readable, git-tracked, and declarative. But YAML is not the whole platform.

The platform needs a data layer, not just a config layer. Recipes need a standard way to persist structured data. Analytics needs a durable store. Coaching needs memory across sessions. Without a standard data layer, every recipe author invents their own storage, every new feature adds another ad hoc file, and the platform fragments.

hookwise should provide a shared data layer that any subsystem or recipe can use. The right tool for that layer -- SQLite, a managed database, purpose-built files -- is an engineering question, not a philosophical one.

### 2. Degrade Gracefully, Always Warn

If any part of hookwise errors, the session continues. hookwise must never accidentally block a tool call due to internal errors. Any unhandled exception in the dispatch pipeline results in `exit 0`.

But graceful degradation must not mean silent degradation. The v1.3 retro showed that "fail-open" was interpreted as "fail-silent" -- 5 of 7 launch bugs shipped unnoticed because errors were swallowed without any signal to the user. Every degradation must produce a visible diagnostic: a status line warning, a cache bus key, a log entry. The user should always know when something is running in a reduced state.

**Degrade gracefully AND always warn the user.**

Graceful degradation is a design practice, not a philosophical argument against infrastructure. A database server that crashes should not take down your Claude Code session -- but the possibility of a crash is not a reason to avoid running a database server. The question is whether the infrastructure solves a real problem and whether hookwise handles its failure modes correctly.

### 3. Infrastructure Is Normal

hookwise is a developer tool platform. Developer tool platforms have databases, background daemons, and managed processes. This is normal and expected.

hookwise already runs a background daemon for feeds, manages PID files, bundles better-sqlite3 as a native addon, and ships a Python TUI package. The bar for adding infrastructure should be: "Does this solve a real problem for users?" Not: "Is this zero-config?" and not: "Could this possibly go down?"

What matters is that infrastructure is invisible to the user when it works, and diagnostic when it does not.

### 4. Guards Should Be Boring; the Platform Should Not

Guard rails should be declarative, predictable, and unexciting. You write YAML rules that read like firewall policies. First block wins. No magic. The exciting part is what you build when you are not worried about what your AI is doing.

This principle applies specifically to the guard system. Other parts of hookwise -- analytics dashboards, coaching nudges, feed-backed status lines, the TUI, recipe ecosystems -- can and should be rich, interactive, and engaging. The platform grows; guards stay boring.

### 5. Recipes Need a Data Contract

A recipe is a shareable hookwise configuration: guards, coaching rules, feed producers, status line segments. Today, a recipe is a directory with `hooks.yaml`. That is enough for declarative configuration but not enough for recipes that need to persist data.

A recipe that tracks code review patterns needs a table. A recipe that scores commit quality needs a rolling window of scores. A recipe that detects dependency drift needs a snapshot of the last lockfile. Without a standard data layer, each recipe author builds their own persistence -- different formats, different locations, different cleanup strategies.

hookwise should define a data contract for recipes: a standard way to create tables, read/write rows, and query data. The underlying engine is an implementation detail. The contract is what makes recipes portable and composable.

### 6. Composition Over Extension

New capabilities should be composable from existing subsystems. A new feed producer is a function. A new guard is a YAML rule. A new recipe is a directory with `hooks.yaml` and optionally a data schema. A new status line segment is a pure function of cache and config.

When composition is not enough and a new architectural layer is needed, that is a signal to design it carefully -- not a signal to avoid it.

---

<- [Back to Home](/)
