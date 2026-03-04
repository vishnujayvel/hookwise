# Philosophy

hookwise was born from watching Claude Code do amazing things -- and occasionally terrifying things.

## Why hookwise Exists

I built hookwise because I kept waking up to disasters.

Not "my server is down" disasters -- "my AI rewrote the build system while I was making coffee" disasters. Claude Code is extraordinary. It can refactor an entire module, set up infrastructure, write tests -- all while you are reading the previous diff. That power is exactly why it needs guard rails.

The existing hooks system is powerful but raw. You write bash scripts, scatter them across your config, and hope they cover the right events. There is no testing, no sharing, and no way to know if your guards actually work until something slips through.

**hookwise is one YAML file that replaces all of that.** Declarative guards that read like firewall rules. Analytics that tell you where your time actually goes. Coaching that nudges you when you have been in "tooling mode" for 90 minutes and forgot your original goal.

## Design Principle

The design principle is simple: **your AI should never be the reason you cannot sleep.** Everything in hookwise serves that principle -- guards protect, context enriches, side effects observe. If any part of hookwise itself errors, it fails open. hookwise must never be the thing that breaks your flow.

> *"Guard rails should be boring. The exciting part is what you build when you are not worried about what your AI is doing."*

## Fail-Open

If any part of hookwise itself errors, it fails open. hookwise must never accidentally block a tool call due to internal errors. Any unhandled exception anywhere in the dispatch pipeline results in `exit 0`.

---

← [Back to Home](/)
