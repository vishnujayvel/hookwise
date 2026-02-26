# Social Media Drafts for hookwise

## Reddit: r/ClaudeAI

**Title:** I built a hooks framework for Claude Code so my AI stops doing terrifying things at 2am

**Body:**

I have been using Claude Code daily for months. It is genuinely incredible — it can refactor entire modules, set up infrastructure, write comprehensive tests. But it also does terrifying things when you are not looking.

My personal greatest hits:
- Force-pushed to main while I was reading its previous diff
- Spent 90 minutes "refactoring" the build system instead of working on the feature I asked for
- Tried to `rm -rf` a directory that definitely should not have been rm -rf'd

Claude Code has a hooks system, but writing individual bash scripts for each guard is painful. So I built **hookwise** — one YAML file that handles everything:

```yaml
guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
```

**What it does:**
- **Guard rails** — Declarative rules that block dangerous commands, require confirmation for risky ones
- **Builder's trap detection** — Warns you when you have been in "tooling mode" too long
- **Session analytics** — SQLite-backed tracking of tool usage, AI authorship ratio, costs
- **Status line** — 19 composable segments showing session info, feeds, and insights
- **12 recipes** — Pre-built patterns you can include with one line
- **1363 tests** — Yeah, I may have gone overboard

It is open source (MIT), works with `npx hookwise init`, and takes about 30 seconds to set up.

GitHub: https://github.com/vishnujayvel/hookwise
npm: `npm install -g hookwise`

Happy to answer questions about the architecture or specific recipes. The three-phase execution model (guards → context → side effects) was the key insight that made everything click.

---

## Reddit: r/programming

**Title:** hookwise: declarative guard rails and analytics for AI code agents (TypeScript, 1363 tests)

**Body:**

**What:** A config-driven hook framework for Claude Code that provides guard rails, session analytics, ambient coaching, and a composable status line — all from one YAML file.

**Why:** AI code agents are powerful but unpredictable. Claude Code's hooks system lets you run shell commands on 13 different events, but managing individual scripts is painful. hookwise gives you:

1. **Declarative guards** — YAML rules with firewall semantics (first match wins). Supports glob patterns, regex matching, and four actions: block, confirm, warn, allow.

2. **Three-phase execution** — Every hook dispatch goes through guards (protect) → context injection (enrich) → side effects (observe). If guards block, phases 2-3 are skipped. Fail-open guarantee: any internal error → exit 0.

3. **Feed platform** — Background daemon polls 5 producers (git, calendar, HN, heartbeat, usage analytics) on staggered intervals, writes to an atomic cache bus with per-key TTL. Status line segments read from cache with `isFresh()` checks.

4. **Testing utilities** — `GuardTester` for in-process rule evaluation, `HookRunner` for subprocess-based dispatch testing, `HookResult` for assertion helpers.

**Tech stack:** TypeScript, vitest (1363 tests), SQLite (analytics), tsup (build), YAML config with deep merge and recipe composition.

**Architecture highlights:**
- Fail-open everywhere — hookwise must never accidentally block a tool call
- Config resolution: global → project → deep merge → recipe includes → env var interpolation
- Cache bus: atomic JSON writes, per-key merge, TTL-aware reads, fail-open on corruption

GitHub: https://github.com/vishnujayvel/hookwise
Install: `npm install -g hookwise && hookwise init`

---

## Reddit: r/artificial

Cross-post from r/ClaudeAI with this intro:

**Title:** I built guard rails for AI code agents after too many 2am disasters

(Same body as r/ClaudeAI post)

---

## LinkedIn

**Title:** Why I built hookwise

I have been pair-programming with Claude Code for months. It writes code faster than I can review it. And that is the problem.

AI code agents are extraordinary — they can refactor modules, set up infrastructure, write tests, all while you are reading the previous change. But they also do things that keep you up at night. Force pushes to main. Dangerous commands. 90-minute tangents into build system refactoring when you asked for a bug fix.

Claude Code has a hooks system. Shell commands that fire on 13 different events. But managing scattered bash scripts with no consistency, no testing, and no sharing? That is not a solution. That is a different problem.

So I built hookwise.

One YAML file. Declarative guard rules that read like a firewall. Session analytics that tell you where your time goes. Coaching that nudges you when you have been in "tooling mode" too long. A composable status line with 19 segments. 12 built-in recipes for common patterns.

The design principle: your AI should never be the reason you cannot sleep.

I went a bit overboard with testing — 1363 tests for 69 source files. But when your tool's job is to prevent disasters, you really want to know it works.

It is open source (MIT), takes 30 seconds to set up, and works with any Claude Code project.

→ GitHub: https://github.com/vishnujayvel/hookwise
→ Install: npx hookwise init

If you are using Claude Code and want guard rails that actually work, give it a try. And if you have ideas for new recipes, I would love to hear them.

#ClaudeCode #AITooling #DeveloperTools #OpenSource #TypeScript

---

## Posting Strategy

1. **Day 1:** Post to r/ClaudeAI (most relevant audience, highest signal-to-noise)
2. **Day 1 + 4 hours:** Cross-post to r/artificial (broader AI audience)
3. **Day 2:** Post to r/programming (technical audience, architecture focus)
4. **Day 2 + 12 hours:** LinkedIn post (professional network, narrative format)

**Tips:**
- Reply to every comment in the first 24 hours
- If asked about architecture, link to the feed platform or three-phase diagrams
- Have `npx hookwise init` ready as the primary CTA
- Mention "1363 tests" — it signals quality and dedication
