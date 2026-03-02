# Hookwise Decision Log

## 2026-02-28: PR #2 CodeRabbit Review Lessons

### Missing file references
**Issue:** `calendar.ts` references `scripts/calendar-feed.py` that didn't exist in the repo.
**Lesson:** Always verify that referenced files exist in the commit. When code spawns or imports a file, that file must be tracked.

### Stale comments
**Issue:** Comment said "four built-in feeds" but code registered five (after insights was added).
**Lesson:** When adding new items to a collection, search for count references in comments and tests. Use grep for numeric words ("four", "five", "4 feeds", "5 feeds").

### Stateful producers violate architecture
**Issue:** `news.ts` used closure variables (`currentIndex`, `lastRotation`) for rotation state instead of reading/writing via the cache bus, violating the documented stateless producer architecture.
**Lesson:** Follow the architecture doc, especially for patterns (stateless producers) that are explicitly called out. When the architecture says "producers are stateless functions", don't sneak state in via closures.
**Fix pattern:** Producer reads previous state from cache bus entry → computes new state → returns data including new state → daemon writes to cache.

### Test classification matters
**Issue:** `tests/e2e/pipeline.test.ts` used mocks for all dependencies but was classified as E2E.
**Lesson:** Test naming should reflect actual test strategy:
- **Unit:** isolated function, mocked dependencies
- **Integration:** multiple real components wired together, some mocks
- **E2E:** real processes, real files, real network (or close to it)
If a test mocks everything, it's not E2E — rename it.

### Fixture data consistency
**Issue:** `fresh-friction.json` had `user_message_count: 25` but only 2 timestamps in `user_message_timestamps`.
**Lesson:** Validate fixture data consistency. Arrays that represent "per-message" data should match the message count. Run a quick sanity check: `count` fields should match their corresponding array lengths.

### Segment count test vs implementation gap
**Issue:** Test expected 19 segments but only 15 were defined (and even those weren't implemented). The 19 was aspirational — likely counting segments planned for the TUI branch.
**Lesson:** Tests should assert what's implemented, not what's planned. Aspirational counts in tests create noise failures that mask real issues.

### PID file TOCTOU race
**Issue:** `isRunning()` check and `writePid()` had a race window where another daemon could start between the check and the write.
**Lesson:** Use OS-level atomic operations (`O_EXCL` flag) for lock-like files. PID files are de facto locks — treat them as such.
