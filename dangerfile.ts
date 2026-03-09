import { danger, warn, fail } from "danger";

const pr = danger.github.pr;

// Require PR description for non-trivial changes
if (pr.body.length < 50 && pr.changed_files > 3) {
  warn("PR has >3 changed files but a short description. Please add context.");
}

// Flag stray files in root
const rootFiles = danger.git.created_files.filter((f) => !f.includes("/"));
const allowedRoot = [
  ".commitlintrc.yml",
  ".coderabbit.yaml",
  ".gitignore",
  ".ls-lint.yml",
  "CLAUDE.md",
  "CONTRIBUTING.md",
  "LICENSE",
  "Makefile",
  "README.md",
  "SECURITY.md",
  "Taskfile.yml",
  "dagger.json",
  "dangerfile.ts",
  "go.mod",
  "go.sum",
  "package.json",
  "package-lock.json",
];
const strayFiles = rootFiles.filter((f) => !allowedRoot.includes(f));
if (strayFiles.length > 0) {
  fail(`Unexpected files in root directory: ${strayFiles.join(", ")}`);
}

// Warn on architecture-critical changes without arch test updates
const archCritical = danger.git.modified_files.filter(
  (f) => f.startsWith("internal/core/") || f.startsWith("internal/feeds/")
);
const archTests = danger.git.modified_files.filter((f) =>
  f.includes("arch_test.go")
);
if (archCritical.length > 0 && archTests.length === 0) {
  warn("Architecture-critical files changed. Consider updating arch tests.");
}

// Require linked issue (or explicit opt-out with "No issue")
const prBody = pr.body || "";
const hasIssueRef =
  prBody.match(/(close[sd]?|fix(e[sd])?|resolve[sd]?)\s+#\d+/i) ||
  prBody.match(/#\d+/) ||
  prBody.match(/no issue/i);
if (!hasIssueRef) {
  warn("No issue reference found in PR description. Add `Closes #XX` or `No issue`.");
}

// Flag large PRs
if (pr.changed_files > 20) {
  warn(`Large PR (${pr.changed_files} files). Consider splitting into smaller PRs.`);
}
