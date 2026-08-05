# PR-14 fix report — diff-as-protocol review findings

Branch: `diff-as-protocol` (worktree at `.claude/worktrees/diff-as-protocol`)
All commits below are on top of `769971e` (HEAD).

## R1 (blocking) — semanticDiff pass 1 duplicate-key cross-section matching

**Change**: `semdiff.go:31-62` (function `semanticDiff`, pass 1 loop). For
each new item, now tries an unmatched *same-section* candidate from
`byKey[n.item.Key]` first, and only falls back to the first unmatched
candidate in any section when no same-section candidate exists.

**Test added**: `semdiff_test.go` —
`TestSemanticDiffDuplicateKeyStaysInSection`. Board with `- nothing yet`
under 🧠/🚧/✅; new version replaces only 🧠's placeholder with `- Fix the
parser`. Asserts output is exactly:
```
added 🧠: "Fix the parser"
removed 🧠: "nothing yet"
```

**Evidence**: test failed pre-fix with fabricated moved lines
(`moved 🧠→🚧: "nothing yet"`, `moved 🚧→✅: "nothing yet"`, `removed ✅:
"nothing yet"`); passes post-fix. `go test ./...` clean.

Commit: `26b1be0`

## R2 (blocking) — unifiedU0 missing size guard

**Change**: `semdiff.go`, function `unifiedU0`. Added the same
common-prefix/common-suffix trim `changedLines` (diff.go:34-52) uses, then a
`1<<20` DP-cell cap over the trimmed middle (`om`/`nm`). On overflow, returns
`[]string{"file changed — too large to diff"}` instead of allocating a full
LCS table. All op/line-number arithmetic downstream (`oi`/`ni`, hunk
anchors, the trailing-newline-only fallback) was rebased onto the trimmed
slices with the `start` offset added back in, so absolute line numbers in
headers stay correct.

**Tests added**: `semdiff_test.go` —
- `TestUnifiedU0PrefixSuffixTrimAbsoluteLineNumbers`: a 10-line file changed
  only at line 6 produces exactly `@@ -6 +6 @@` / `-line6` / `+CHANGED`.
- `TestUnifiedU0OverflowFallsBackToSingleLine`: two 1100-distinct-line
  synthetic middles (1100×1100 > 2^20) wrapped in a shared prefix/suffix line;
  asserts the single `"file changed — too large to diff"` line.

**Evidence**: overflow test failed pre-fix (produced a full multi-thousand-
line diff instead of the fallback); both tests pass post-fix. `go test
./...` clean.

Commit: `0b6ec21`

## R3 — `[g]` .gitignore branch doesn't exclude `.sidecar/`

**Change**: `init.go`, `offerGitExclude`'s `case "g":` branch (around
line 588) now also calls a new `excludeCustomPathSnapshotDirTo(gitignore,
rel)` (added next to the existing `excludeCustomPathSnapshotDir`), which
writes the `.sidecar/` snapshot-dir entry directly to the given ignore file
via the existing idempotent `writeIgnore`/`appendLine`.

**Test added**: `init_test.go` —
`TestOfferGitExcludeGitignoreBranchExcludesSnapshotDir` (plus a new
`withStdin` test helper to feed `"g\n"` into the interactive prompt).
Asserts the resulting `.gitignore` contains both the board file and
`.sidecar/`.

**Evidence**: test failed pre-fix (`.gitignore` missing `.sidecar/`); passes
post-fix. `go test ./...` clean.

Commit: `997057c`

## R4 — closingReminder on an unparseable board yields "Sections: ."

**Change**: `init.go`, `reconcileMessageLabels`. When `labels` is empty, the
function now returns the base sentence only, dropping the `" Sections:
…"` clause entirely rather than joining an empty slice into `"Sections:
."`. The sentinel phrase (`hookSentinel`, "the sidecar review queue") is
unaffected either way.

**Tests added**:
- `init_test.go` — `TestReconcileMessageLabelsEmptyDropsSectionsClause`:
  direct unit test on `reconcileMessageLabels(rel, nil)`.
- `diffcmd_test.go` — `TestRunDiffUnparseableBoardOmitsSectionsClause`:
  end-to-end via `runDiff` on an unparseable board (no `##` headings); checks
  the diff output's last line contains the sentinel and not `"Sections:"`.

**Evidence**: both tests failed pre-fix with `... Sections: .` on the last
line; pass post-fix. `go test ./...` clean.

Commit: `91faa80`

## R5 — writeSnapshot swallows errors

**Change**: `diffcmd.go`, `writeSnapshot`. Both the `MkdirAll` and
`WriteFile` error paths now `fmt.Fprintln(os.Stderr, "sidecar diff:", err)`
before returning; exit code is untouched (still 0 — a hook must never fail
the turn).

**Test added**: none, per the finding's instruction ("No test required").

**Evidence**: `go build ./...`, `go vet ./...`, `go test ./...` all clean.

Commit: `d38506e`

## R6 — unifiedU0 hunk headers should carry the nearest preceding heading

**Change**: `semdiff.go`, function `unifiedU0`. Added `headingBefore(o
[]string, limit int) string`, which returns the last line in `o[:limit]`
starting with `"#"`. Each hunk now computes `anchorIdx` — the 0-based count
of old lines strictly preceding the hunk, captured *before* the existing
zero→1 display-only adjustment so the heading lookup sees the true anchor —
and appends `" " + heading` to the rendered header when non-empty, e.g. `@@
-5 +4,0 @@ ## 🚧 In progress`.

**Tests added/extended**: `semdiff_test.go` —
`TestUnifiedU0HeadingContext`: a two-section board where only the second
section's item text changes; asserts the hunk header contains `## 🚧 In
progress`. Existing exact-match `TestUnifiedU0`/`TestUnifiedU0Insert`/etc.
inputs remain heading-free (no `#`-prefixed lines) so their exact-string
assertions are unaffected.

**Evidence**: `go test ./...` clean, including all prior unifiedU0 exact-
match tests unchanged.

Commit: `0b6ec21` (folded into the same commit as R2 — both touch
`unifiedU0` and were implemented together)

## R7 (nits) — dead TrimLeft; single-line HTML comments not skipped

**Change**: `board.go`:
- Bullet guard simplified from
  `strings.HasPrefix(strings.TrimLeft(ln, " \t"), "- ") &&
  !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t")` to
  `strings.HasPrefix(ln, "- ")` (the negated guards already rule out a
  leading space/tab, so the `TrimLeft` was a no-op).
- `parseBoard`'s comment handling: a line starting with `<!--` now always
  `continue`s (skipped), setting `inComment = true` only when it doesn't
  also close on the same line (`!strings.Contains(trimmed, "-->")`).
  Previously a complete single-line comment fell through to the bullet/
  continuation switch and got appended to the preceding item's `Raw` as a
  continuation line, so editing just the comment reported a spurious
  `edited` on the item above it.

**Test added**: `board_test.go` —
`TestParseBoardSingleLineCommentSkipped`: a board with `- Review PR #7`,
then `<!-- a note -->` on its own line, then `- Ship v2`; asserts 2 items
parse and the first item's `Raw` is exactly `- Review PR #7` (comment text
didn't leak in as a continuation line).

**Evidence**: test failed pre-fix (`Raw` contained the comment line
appended); passes post-fix. `go test ./...` clean. The bullet-guard
simplification is a pure refactor verified by the full existing suite,
including `TestParseBoardTabIndentedSubBullet`.

Commit: `769971e`

## Final verification

```
gofmt -l .        # clean, no output
go vet ./...      # clean
go test ./...     # ok  github.com/than/sidecar
```

All 7 findings fixed across 6 commits (R2+R6 share one commit since both
touch `unifiedU0`), each with a regression test written and watched failing
before the fix, per TDD, except R5 which the task explicitly marked as not
requiring one.
