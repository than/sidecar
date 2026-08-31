package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runInit is the int-only form these tests use — main() is the only
// production caller and it needs the path runInitBoard returns.
func runInit(args []string) int {
	code, _ := runInitBoard(args)
	return code
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// runInit scaffolds the file with the starter template when it's absent, and
// (outside a git work tree) doesn't touch stdin.
func TestInitScaffolds(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "SIDECAR.md")

	if code := runInit([]string{target}); code != 0 {
		t.Fatalf("runInit exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# Sidecar") {
		t.Errorf("scaffolded file missing header:\n%s", data)
	}
	for _, marker := range []string{"🧠", "🤖", "🚧", "🚘", "✅", "📦"} {
		if !strings.Contains(string(data), marker) {
			t.Errorf("template missing section marker %q", marker)
		}
	}
}

// An existing file is left untouched.
func TestInitLeavesExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "SIDECAR.md")
	if err := os.WriteFile(target, []byte("# Mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runInit([]string{target}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "# Mine\n" {
		t.Errorf("existing file overwritten: %q", data)
	}
}

func TestWriteClaudeNoteAppendsAndDedupes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeClaudeNote(root, "SIDECAR.md", defaultSections())
	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	got := string(data)
	for _, want := range []string{"# Existing", claudeNoteMarker, "Maintain `SIDECAR.md`", "go install github.com/than/sidecar@latest", "🧠"} {
		if !strings.Contains(got, want) {
			t.Errorf("CLAUDE.md missing %q:\n%s", want, got)
		}
	}

	// Second call must not duplicate the note.
	writeClaudeNote(root, "SIDECAR.md", defaultSections())
	data, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if n := strings.Count(string(data), "<!-- "+claudeNoteMarker+" -->"); n != 1 {
		t.Errorf("note written %d times, want 1", n)
	}
}

// A fresh project (no settings.json) gets a UserPromptSubmit hook written.
func TestReconcileHookFreshFile(t *testing.T) {
	root := t.TempDir()
	writeReconcileHook(root, "SIDECAR.md", defaultSections())

	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.json not valid JSON: %v\n%s", err, data)
	}
	hooks := s["hooks"].(map[string]any)
	if _, ok := hooks["UserPromptSubmit"]; !ok {
		t.Errorf("no UserPromptSubmit hook:\n%s", data)
	}
	if _, ok := hooks["SessionStart"]; ok {
		t.Errorf("unexpected SessionStart hook:\n%s", data)
	}
	if !strings.Contains(string(data), "sidecar SIDECAR.md") {
		t.Errorf("hook missing sidecar reference:\n%s", data)
	}
}

// Merging preserves a user's own hooks, upgrades an old sidecar SessionStart
// hook to UserPromptSubmit, and is idempotent across re-runs.
func TestReconcileHookMergesAndUpgrades(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")

	// Pre-existing settings: a user hook plus an old sidecar SessionStart hook.
	existing := map[string]any{
		"model": "opus",
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo keep-me"}}},
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo Maintain SIDECAR.md as " + hookSentinel}}},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	writeReconcileHook(root, "SIDECAR.md", defaultSections())
	writeReconcileHook(root, "SIDECAR.md", defaultSections()) // second run must not duplicate

	data, _ := os.ReadFile(path)
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.json not valid JSON: %v\n%s", err, data)
	}
	if s["model"] != "opus" {
		t.Errorf("unrelated key 'model' lost:\n%s", data)
	}
	if !strings.Contains(string(data), "keep-me") {
		t.Errorf("user's own hook was dropped:\n%s", data)
	}
	hooks := s["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; !ok {
		t.Errorf("user's SessionStart hook removed entirely:\n%s", data)
	}
	ups, ok := hooks["UserPromptSubmit"].([]any)
	if !ok || len(ups) != 1 {
		t.Fatalf("want exactly 1 UserPromptSubmit entry, got:\n%s", data)
	}
	// The old sidecar SessionStart hook should be gone (only keep-me remains).
	if n := strings.Count(string(data), hookSentinel); n != 1 {
		t.Errorf("sentinel appears %d times, want 1 (old hook not replaced):\n%s", n, data)
	}
}

// A settings.json that isn't valid JSON is left untouched.
func TestReconcileHookLeavesInvalidJSON(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeReconcileHook(root, "SIDECAR.md", defaultSections())

	data, _ := os.ReadFile(path)
	if string(data) != "{not json" {
		t.Errorf("invalid settings.json was modified: %q", data)
	}
}

func TestAppendLineDedupAndNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclude")

	// Missing trailing newline should be repaired before appending.
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(path, "SIDECAR.md"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "existing\nSIDECAR.md\n" {
		t.Errorf("got %q, want %q", got, "existing\nSIDECAR.md\n")
	}

	// Second identical append is a no-op.
	if err := appendLine(path, "SIDECAR.md"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); strings.Count(string(got), "SIDECAR.md") != 1 {
		t.Errorf("duplicate line written: %q", got)
	}
}

func TestClaudeNoteCustomSections(t *testing.T) {
	secs := []Section{
		{"🧠", "Needs action", "for the human"},
		{"", "Todo", ""},
	}
	note := claudeNote("SIDECAR.md", secs)
	for _, want := range []string{
		claudeNoteMarker,
		"- `## 🧠 Needs action` — for the human",
		"`## Todo`",
		"Maintain `SIDECAR.md`",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "Todo` — ") {
		t.Errorf("hintless section should have no ' — meaning':\n%s", note)
	}
}

// The non-interactive path must still write the default template and must
// NOT start a picker (tests aren't a TTY).
// Q2: an unknown flag must not be silently treated as a filename — it must
// be rejected, not create a file named after the flag.
func TestRunInitUnknownFlagRejected(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir, func() {
		var code int
		errOut := captureStderr(t, func() {
			captureStdout(t, func() {
				code = runInit([]string{"--dry-run"})
			})
		})
		if code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
		if !strings.Contains(errOut, `unknown flag "--dry-run"`) {
			t.Errorf("stderr = %q, want it to mention the unknown flag", errOut)
		}
		if _, err := os.Stat(filepath.Join(dir, "--dry-run")); err == nil {
			t.Error("a file named after the unknown flag was created")
		}
	})
}

// Q4: `sidecar init -h`/`--help` must print usage and exit 0, not fall
// through to the unknown-flag rejection.
func TestRunInitHelpFlag(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		dir := t.TempDir()
		withWorkDir(t, dir, func() {
			var code int
			out := captureStdout(t, func() {
				code = runInit([]string{flag})
			})
			if code != 0 {
				t.Errorf("%s: exit = %d, want 0", flag, code)
			}
			if !strings.Contains(out, "usage: sidecar init") {
				t.Errorf("%s: out = %q, want usage line", flag, out)
			}
			if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err == nil {
				t.Errorf("%s: board was created instead of just printing help", flag)
			}
		})
	}
}

// W2: --yes/-y are not a pure no-op — they still gate the interactive
// section picker (interactiveTTY() alone isn't a non-blocking guarantee, so
// a caller that wants a guaranteed-non-interactive init should still pass
// --yes). The help text must say so rather than call it a no-op.
func TestRunInitHelpDocumentsYesSkipsPicker(t *testing.T) {
	dir := t.TempDir()
	var out string
	withWorkDir(t, dir, func() {
		out = captureStdout(t, func() {
			runInit([]string{"-h"})
		})
	})
	if !strings.Contains(out, "skip the section picker") {
		t.Errorf("help text doesn't document --yes/-y as skipping the picker:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "--yes") && strings.Contains(line, "no-op") {
			t.Errorf("--yes/-y help line still calls it a no-op: %q", line)
		}
	}
}

func TestInitNonInteractiveUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "SIDECAR.md")
	if code := runInit([]string{target}); code != 0 {
		t.Fatalf("runInit exit = %d", code)
	}
	data, _ := os.ReadFile(target)
	for _, s := range defaultSections() {
		if !strings.Contains(string(data), s.Header()) {
			t.Errorf("default template missing %q:\n%s", s.Header(), data)
		}
	}
}

func TestReconcileMessageCustomSections(t *testing.T) {
	secs := []Section{{"🧠", "Needs action", ""}, {"✅", "Done", ""}}
	msg := reconcileMessage("SIDECAR.md", secs)
	if !strings.Contains(msg, "Sections: 🧠 Needs action / ✅ Done.") {
		t.Errorf("reconcile message section list wrong:\n%s", msg)
	}
	if !strings.Contains(msg, hookSentinel) {
		t.Errorf("reconcile message missing sentinel:\n%s", msg)
	}
}

// R4: an empty labels list (unparseable board) must drop the " Sections: …"
// clause entirely rather than render "Sections: .", while keeping the
// sentinel phrase intact.
func TestReconcileMessageLabelsEmptyDropsSectionsClause(t *testing.T) {
	msg := reconcileMessageLabels("SIDECAR.md", nil)
	if strings.Contains(msg, "Sections:") {
		t.Errorf("expected no Sections clause for empty labels:\n%s", msg)
	}
	if !strings.Contains(msg, hookSentinel) {
		t.Errorf("reconcile message missing sentinel:\n%s", msg)
	}
}

func TestExcludeSidecarDir(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	excludeSidecarDir(dir, true)
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(data), ".sidecar/") {
		t.Fatalf("info/exclude = %q, err %v", data, err)
	}
	// Idempotent: a second call adds nothing.
	excludeSidecarDir(dir, true)
	again, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if strings.Count(string(again), ".sidecar/") != 1 {
		t.Errorf("exclude entry duplicated:\n%s", again)
	}
}

// Q3: excludeSidecarDir must print the same confirmation line every other
// exclude path prints, instead of succeeding silently — when verbose.
func TestExcludeSidecarDirPrintsConfirmation(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	out := captureStdout(t, func() { excludeSidecarDir(dir, true) })
	if !strings.Contains(out, `Added ".sidecar/"`) {
		t.Errorf("out = %q, want a confirmation line like the other exclude paths", out)
	}
}

// The residual from re-review: writeSnapshot's fresh-dir exclusion runs on
// every plain `sidecar diff` hook invocation and must stay silent on stdout.
func TestExcludeSidecarDirQuietPrintsNothing(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	out := captureStdout(t, func() { excludeSidecarDir(dir, false) })
	if out != "" {
		t.Errorf("out = %q, want no stdout output when verbose=false", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(data), ".sidecar/") {
		t.Fatalf("info/exclude = %q, err %v — the exclude itself must still happen", data, err)
	}
}

// S1: a custom board resident inside .sidecar/ (e.g. .sidecar/notes.md) must
// route to the automatic .sidecar/-dir exclude, never the custom-path
// prompt — reachable directly via offerGitExclude (e.g. from offerCreate in
// viewer mode), not just through runInit's own top-level guard.
func TestOfferGitExcludeSidecarResidentPathAutomatic(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	sidecarDir := filepath.Join(dir, sidecarDirName)
	os.MkdirAll(sidecarDir, 0o755)
	target := filepath.Join(sidecarDir, "notes.md")
	os.WriteFile(target, []byte("# notes\n"), 0o644)

	out := withStdinCapture(t, "", func() { offerGitExclude(target) })
	if strings.Contains(out, "Keep") && strings.Contains(out, "out of git?") {
		t.Errorf("prompted for a .sidecar/-resident path:\n%s", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if !strings.Contains(string(data), sidecarDirName+"/") {
		t.Errorf("info/exclude missing %s/: %q", sidecarDirName, data)
	}
	if strings.Contains(string(data), "notes.md") {
		t.Errorf("info/exclude should hold the whole %s/ dir, not the file itself: %q", sidecarDirName, data)
	}
}

// S1: same guard for the non-interactive (--yes) path.
func TestGitExcludeDefaultSidecarResidentPathAutomatic(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	sidecarDir := filepath.Join(dir, sidecarDirName)
	os.MkdirAll(sidecarDir, 0o755)
	target := filepath.Join(sidecarDir, "notes.md")
	os.WriteFile(target, []byte("# notes\n"), 0o644)

	gitExcludeDefault(target)
	data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if !strings.Contains(string(data), sidecarDirName+"/") {
		t.Errorf("info/exclude missing %s/: %q", sidecarDirName, data)
	}
	if strings.Contains(string(data), "notes.md") {
		t.Errorf("info/exclude should hold the whole %s/ dir, not the file itself: %q", sidecarDirName, data)
	}
}

// withStdinCapture combines withStdin and captureStdout: runs f with stdin
// set to input and returns whatever f printed.
func withStdinCapture(t *testing.T, input string, f func()) string {
	t.Helper()
	var out string
	withStdin(t, input, func() {
		out = captureStdout(t, f)
	})
	return out
}

// withStdin redirects os.Stdin to input for the duration of f, restoring it
// afterward.
func withStdin(t *testing.T, input string, f func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	f()
}

// R3: the [g] .gitignore branch of offerGitExclude must also exclude the
// .sidecar/ snapshot dir that `sidecar diff` creates beside a custom board
// path — not just the board file itself.
func TestOfferGitExcludeGitignoreBranchExcludesSnapshotDir(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	target := filepath.Join(dir, "notes.md")
	os.WriteFile(target, []byte("# notes\n"), 0o644)

	withStdin(t, "g\n", func() {
		offerGitExclude(target)
	})

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), "notes.md") {
		t.Errorf(".gitignore missing board file: %q", data)
	}
	if !strings.Contains(string(data), sidecarDirName+"/") {
		t.Errorf(".gitignore missing %s/ snapshot dir: %q", sidecarDirName, data)
	}
}

// T3: the guard must prove the installed sidecar actually has the diff
// subcommand (an old binary falls into viewer mode on `sidecar diff` and can
// hang a TTY-inheriting hook) — `command -v sidecar` alone doesn't prove
// that. The probe itself must redirect stdin from /dev/null so an old
// binary's viewer-mode fallback fails fast instead of hanging on the probe.
func TestReconcileHookEntryGuarded(t *testing.T) {
	entry := reconcileHookEntry(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	cmd := entry["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "sidecar diff --help") {
		t.Errorf("hook not guarded by a real feature probe: %q", cmd)
	}
	if !strings.Contains(cmd, "</dev/null") {
		t.Errorf("probe missing </dev/null redirect (old binary could hang): %q", cmd)
	}
	if !strings.Contains(cmd, "sidecar diff") {
		t.Errorf("hook missing the actual diff invocation: %q", cmd)
	}
	if !strings.Contains(cmd, hookSentinel) {
		t.Errorf("hook fallback lost the sentinel: %q", cmd)
	}
	if strings.Contains(cmd, "sidecar diff '") {
		t.Errorf("default board should not pass an explicit path: %q", cmd)
	}
}

func TestReconcileHookEntryCustomPathPassed(t *testing.T) {
	entry := reconcileHookEntry("NOTES.md", defaultSections())
	cmd := entry["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "sidecar diff 'NOTES.md'") {
		t.Errorf("custom path missing from hook: %q", cmd)
	}
}

func TestReplaceClaudeNote(t *testing.T) {
	old := "# My project\n\n<!-- sidecar:review-queue -->\nold sidecar text\n<!-- /sidecar:review-queue -->\n\n## Other section\n"
	note := claudeNote(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	got, replaced := replaceClaudeNote(old, note)
	if !replaced {
		t.Fatal("expected replacement")
	}
	if strings.Contains(got, "old sidecar text") {
		t.Error("stale note survived")
	}
	if !strings.Contains(got, ".sidecar/sidecar.md") || !strings.Contains(got, "# My project") || !strings.Contains(got, "## Other section") {
		t.Errorf("replacement damaged surrounding content:\n%s", got)
	}
}

func TestReplaceClaudeNoteNoMarker(t *testing.T) {
	if _, replaced := replaceClaudeNote("# Plain file\n", "note"); replaced {
		t.Error("replaced without a marker")
	}
}

func TestClaudeNoteWritingRules(t *testing.T) {
	note := claudeNote(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	for _, want := range []string{"Apple Developer documentation voice", "two sentences of detail", "bare URLs, each on its own line", "`Next:` line", "never hard-wrap; the viewer wraps to the pane"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q", want)
		}
	}
	// Abstract rules alone let agents write status-as-story and file it under
	// the human-action section. The note carries the placement rule and one
	// worked wrong→right pair.
	// Agents were citing the board and sidecar itself in PR bodies and commit
	// messages, where the reader has neither.
	for _, want := range []string{"where things stand, not how they got there", "only holds items where the human is the blocker", "Split by who acts (right)", "private channel between you and the human", "any other shared artifact", "every item there ends with a `Next:` line", "A finding, a question, or a status names no action"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q", want)
		}
	}
}

// The note names the first configured section in its placement rule, so a
// custom section set must not leave the rule pointing at the built-in "🧠
// Needs action".
// The placement rule is about the human-blocker section specifically, so it
// has to follow that section rather than whatever sits at index 0. The picker
// reorders, renames, re-emojis and deletes sections; position carries no role.
func TestClaudeNotePlacementRuleFollowsTheRole(t *testing.T) {
	// Reordered and renamed, but 🧠 is still there: the rule names it, at its
	// new heading, not the section that now happens to be first.
	moved := []Section{
		{"🚧", "In progress", "actively being worked"},
		{"🧠", "For me", "the human is the blocker"},
		{"🤖", "Queue", "queued for an agent, not started"},
	}
	note := claudeNote("board.md", moved)
	if !strings.Contains(note, "`## 🧠 For me` only holds items") {
		t.Errorf("rule doesn't follow the renamed 🧠 section:\n%s", note)
	}
	if strings.Contains(note, "`## 🚧 In progress` only holds items") {
		t.Errorf("rule attached to whatever is first:\n%s", note)
	}

	// The human dropped the human section entirely. Saying nothing is right;
	// asserting that some other section holds their blockers is not.
	dropped := []Section{{"", "Todo", "stuff"}, {"", "Doing", "in flight"}}
	note = claudeNote("board.md", dropped)
	if strings.Contains(note, "only holds items where the human is the blocker") {
		t.Errorf("placement rule emitted with no human section:\n%s", note)
	}
	if strings.Contains(note, "Split by who acts") {
		t.Errorf("worked example emitted with no human section:\n%s", note)
	}
	for _, leak := range []string{"🧠", "Needs you"} {
		if strings.Contains(note, leak) {
			t.Errorf("note leaked default section %q into a custom set:\n%s", leak, note)
		}
	}
}

func TestMigrateLegacyBoard(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- carry me over\n"), 0o644)
	mustRun(t, dir, "git", "add", "SIDECAR.md")

	if !migrateLegacyBoard(dir) {
		t.Fatal("expected migration")
	}
	data, err := os.ReadFile(filepath.Join(dir, sidecarDirName, "sidecar.md"))
	if err != nil || !strings.Contains(string(data), "carry me over") {
		t.Fatalf("board content lost: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SIDECAR.md")); !os.IsNotExist(err) {
		t.Error("legacy file still present")
	}
	// No longer tracked.
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "SIDECAR.md")
	cmd.Dir = dir
	if cmd.Run() == nil {
		t.Error("SIDECAR.md still tracked after migration")
	}
}

func TestMigrateLegacyBoardUntracked(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- local only\n"), 0o644)
	if !migrateLegacyBoard(dir) {
		t.Fatal("expected migration of an untracked board")
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not moved")
	}
}

func TestMigrateLegacyBoardNothingToDo(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if migrateLegacyBoard(dir) {
		t.Error("migrated with no legacy file present")
	}
}

// F1: re-running init against an existing board with custom sections must
// derive the note/hook sections from the board itself, not the default five.
func TestRunInitExistingBoardKeepsCustomSections(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	board := "## 🔥 Hot\n\n- nothing yet\n\n## 🧊 Cold\n\n- nothing yet\n"
	if err := os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sidecarDirName, "sidecar.md"), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkDir(t, dir, func() {
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	for _, want := range []string{"🔥 Hot", "🧊 Cold"} {
		if !strings.Contains(string(claude), want) {
			t.Errorf("CLAUDE.md note missing %q:\n%s", want, claude)
		}
	}
	for _, unwanted := range []string{"Needs action", "In progress", "Parked"} {
		if strings.Contains(string(claude), unwanted) {
			t.Errorf("CLAUDE.md note kept default section %q:\n%s", unwanted, claude)
		}
	}
	settings, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(string(settings), "🔥 Hot / 🧊 Cold") {
		t.Errorf("hook fallback missing custom sections:\n%s", settings)
	}
}

// F2: the closing hint prints bare "sidecar" for the default board, and the
// path as given for a custom one.
func TestWatchItHint(t *testing.T) {
	dir2 := t.TempDir()
	mustRun(t, dir2, "git", "init", "-q")
	withWorkDir(t, dir2, func() {
		out := captureStdout(t, func() {
			if code := runInit([]string{"--yes"}); code != 0 {
				t.Fatalf("exit = %d", code)
			}
		})
		if !strings.Contains(out, "Watch it:  sidecar\n") {
			t.Errorf("default target should print bare 'sidecar':\n%s", out)
		}
	})

	dir3 := t.TempDir()
	mustRun(t, dir3, "git", "init", "-q")
	withWorkDir(t, dir3, func() {
		out := captureStdout(t, func() {
			if code := runInit([]string{"notes.md", "--yes"}); code != 0 {
				t.Fatalf("exit = %d", code)
			}
		})
		if !strings.Contains(out, "Watch it:  sidecar notes.md\n") {
			t.Errorf("custom target should print the given path:\n%s", out)
		}
	})
}

// F3: init from a subdirectory of a repo with a root-level SIDECAR.md must
// not migrate it — migration only applies when the target's cwd is the
// directory holding the legacy file.
func TestInitFromSubdirDoesNotMigrateRootBoard(t *testing.T) {
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- root board\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkDir(t, sub, func() {
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	if _, err := os.Stat(filepath.Join(root, "SIDECAR.md")); err != nil {
		t.Error("root SIDECAR.md was moved/removed from a subdir init")
	}
	if _, err := os.Stat(filepath.Join(root, sidecarDirName, "sidecar.md")); err == nil {
		t.Error("migration incorrectly ran against the git root instead of cwd")
	}
	if _, err := os.Stat(filepath.Join(sub, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not scaffolded in the subdir as targeted")
	}
}

// F7: the migration message prints a path relative to the root argument,
// not an absolute one.
func TestMigrateLegacyBoardMessageIsRelative(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if !migrateLegacyBoard(dir) {
			t.Fatal("expected migration")
		}
	})
	wantRel := filepath.Join(sidecarDirName, "sidecar.md")
	if !strings.Contains(out, wantRel) {
		t.Errorf("message missing relative path %q:\n%s", wantRel, out)
	}
	if strings.Contains(out, dir) {
		t.Errorf("message printed an absolute path:\n%s", out)
	}
}

// F6: after a migration, runInit must not also print the "already exists"
// line for the same os.Stat branch — that would read as a contradiction.
func TestRunInitSuppressesAlreadyExistsAfterMigration(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out string
	withWorkDir(t, dir, func() {
		out = captureStdout(t, func() {
			if code := runInit([]string{"--yes"}); code != 0 {
				t.Fatalf("exit = %d", code)
			}
		})
	})
	if !strings.Contains(out, "Moved") {
		t.Errorf("expected a migration message:\n%s", out)
	}
	if strings.Contains(out, "already exists") {
		t.Errorf("duplicate 'already exists' message after migration:\n%s", out)
	}
}

// U1: re-running init over an existing default board must not silently drop
// each section's " — hint" line from the CLAUDE.md note — sectionsFromBoard
// derives Sections from the rendered heading alone (no Hint recoverable
// there), so it must backfill Hint from defaultSections() when the label
// matches exactly.
func TestRunInitRerunRoundTripsHints(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	withWorkDir(t, dir, func() {
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("first run exit = %d", code)
		}
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("second run exit = %d", code)
		}
	})
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range defaultSections() {
		if s.Hint == "" {
			continue
		}
		want := "— " + s.Hint
		if !strings.Contains(string(data), want) {
			t.Errorf("CLAUDE.md missing hint %q after re-run:\n%s", want, data)
		}
	}
}

// V1: outside a git work tree, repoRoot(filepath.Dir(abs)) for the default
// board (.sidecar/sidecar.md) falls back to .sidecar/ itself (no git
// toplevel to override it) — CLAUDE.md and .claude/settings.json must still
// land at the project root, not inside .sidecar/, and the hook must
// reference the default board bare ('sidecar diff', no explicit path).
func TestRunInitYesNonGitDefaultBoardSeedsAtRoot(t *testing.T) {
	dir := t.TempDir() // deliberately no `git init`
	withWorkDir(t, dir, func() {
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Fatal("board not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Error("CLAUDE.md not written at the top level")
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md wrongly written inside .sidecar/")
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not written at .claude/settings.json: %v", err)
	}
	if !strings.Contains(string(data), "sidecar diff") {
		t.Error("hook missing sidecar diff")
	}
	if strings.Contains(string(data), "sidecar diff sidecar.md") || strings.Contains(string(data), "sidecar diff '") {
		t.Errorf("hook references a bad relative path instead of the bare default board:\n%s", data)
	}
}

func TestRunInitYesNonInteractive(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	withWorkDir(t, dir, func() {
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not created")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude")); !strings.Contains(string(data), ".sidecar/") {
		t.Error(".sidecar/ not excluded")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); !strings.Contains(string(data), "sidecar:review-queue") {
		t.Error("CLAUDE.md note not written")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json")); !strings.Contains(string(data), "sidecar diff") {
		t.Error("hook not written")
	}
}

// Issue #16: bare `sidecar init` in a fresh git repo must install everything
// — board, git-exclude, CLAUDE.md note, reconcile hook — without reading a
// single answer from stdin (stdin stays empty; a prompt read would hang on a
// real, non-piped run and here would just return "" — the test instead
// checks no prompt text was printed at all).
func TestInitBareInstallsEverythingNoPrompts(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	var code int
	var out string
	withWorkDir(t, dir, func() {
		withStdin(t, "", func() {
			out = captureStdout(t, func() {
				code = runInit(nil)
			})
		})
	})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, unwanted := range []string{"[Y/n]", "[B/c/n]", "[E/g/n]", "Choice"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("bare init printed a prompt %q:\n%s", unwanted, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not created")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude")); !strings.Contains(string(data), ".sidecar/") {
		t.Error(".sidecar/ not excluded")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); !strings.Contains(string(data), "sidecar:review-queue") {
		t.Error("CLAUDE.md note not written")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json")); !strings.Contains(string(data), "sidecar diff") {
		t.Error("hook not written")
	}
}

// Issue #16: a root-level SIDECAR.md is migrated silently by bare init — no
// [Y/n] prompt, no need for a TTY to answer it.
func TestInitBareMigratesLegacySilently(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- carry me over\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", "SIDECAR.md")

	var code int
	var out string
	withWorkDir(t, dir, func() {
		withStdin(t, "", func() {
			out = captureStdout(t, func() {
				code = runInit(nil)
			})
		})
	})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out, "[Y/n]") {
		t.Errorf("migration prompt printed on bare init:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, sidecarDirName, "sidecar.md"))
	if err != nil || !strings.Contains(string(data), "carry me over") {
		t.Fatalf("board content lost: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SIDECAR.md")); !os.IsNotExist(err) {
		t.Error("legacy file still present")
	}
}

// Issue #16: --no-claude installs the board (and exclude) but leaves
// CLAUDE.md and .claude/settings.json untouched.
func TestInitNoClaudeSkipsNoteAndHook(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	withWorkDir(t, dir, func() {
		withStdin(t, "", func() {
			captureStdout(t, func() {
				if code := runInit([]string{"--no-claude"}); code != 0 {
					t.Fatalf("exit = %d", code)
				}
			})
		})
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("CLAUDE.md was written despite --no-claude")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Error(".claude/settings.json was written despite --no-claude")
	}
}

// Issue #16 (W1): --keep-board leaves a legacy root SIDECAR.md exactly where
// it is and targets init against it directly — no competing
// .sidecar/sidecar.md is scaffolded, since that would silently shadow the
// kept board (defaultBoardPath prefers the .sidecar/ home). Note, hook, and
// exclude wire against SIDECAR.md itself, via the same code paths a custom
// board path already uses.
func TestInitKeepBoardSkipsMigration(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- stay put\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkDir(t, dir, func() {
		withStdin(t, "", func() {
			captureStdout(t, func() {
				if code := runInit([]string{"--keep-board"}); code != 0 {
					t.Fatalf("exit = %d", code)
				}
			})
		})
	})
	data, err := os.ReadFile(filepath.Join(dir, "SIDECAR.md"))
	if err != nil || !strings.Contains(string(data), "stay put") {
		t.Fatalf("legacy board disturbed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err == nil {
		t.Error(".sidecar/sidecar.md was scaffolded, shadowing the kept legacy board")
	}
	withWorkDir(t, dir, func() {
		if got := defaultBoardPath(); got != "SIDECAR.md" {
			t.Errorf("defaultBoardPath() = %q, want SIDECAR.md to still resolve as the board", got)
		}
	})
	// Note/hook wired against the legacy board's own path, not the default.
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(claude), "Maintain `SIDECAR.md`") {
		t.Errorf("CLAUDE.md note not wired to SIDECAR.md:\n%s", claude)
	}
	settings, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(string(settings), "sidecar diff 'SIDECAR.md'") {
		t.Errorf("reconcile hook not wired to SIDECAR.md:\n%s", settings)
	}
}

// Round 2, X2: when BOTH a legacy root SIDECAR.md and a .sidecar/sidecar.md
// already exist, --keep-board must NOT retarget init at the legacy file —
// defaultBoardPath() still prefers .sidecar/sidecar.md, so wiring the
// note/hook/"Watch it" hint to SIDECAR.md would point them at a board the
// viewer won't open. The legacy file is still left untouched (no migration),
// but init proceeds against the default board as usual.
func TestInitKeepBoardBothBoardsPresentKeepsDefaultTarget(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sidecarDirName, "sidecar.md"), []byte("## 🧠 Needs action\n\n- current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkDir(t, dir, func() {
		withStdin(t, "", func() {
			captureStdout(t, func() {
				if code := runInit([]string{"--keep-board"}); code != 0 {
					t.Fatalf("exit = %d", code)
				}
			})
		})
	})
	// Legacy file untouched — no migration.
	if data, err := os.ReadFile(filepath.Join(dir, "SIDECAR.md")); err != nil || !strings.Contains(string(data), "legacy") {
		t.Fatalf("legacy board disturbed: %q, %v", data, err)
	}
	// The default board is the one wired up, not the legacy file.
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(claude), "Maintain `"+filepath.Join(sidecarDirName, "sidecar.md")+"`") {
		t.Errorf("CLAUDE.md note not wired to the default board:\n%s", claude)
	}
	if strings.Contains(string(claude), "Maintain `SIDECAR.md`") {
		t.Errorf("CLAUDE.md note wired to the legacy board instead of the default:\n%s", claude)
	}
	settings, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(string(settings), "sidecar diff --help") || strings.Contains(string(settings), "sidecar diff 'SIDECAR.md'") {
		t.Errorf("reconcile hook not wired to the default board:\n%s", settings)
	}
}

// Round 2, X3: a git-tracked legacy board kept via --keep-board can't
// usefully go into .git/info/exclude (git doesn't stop tracking a file just
// because it's ignored) — init must print the untrack command instead of a
// misleading "Added …" confirmation, and must not write a no-op exclude
// entry.
func TestInitKeepBoardTrackedLegacyPrintsUntrackHint(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", "SIDECAR.md")

	var out string
	withWorkDir(t, dir, func() {
		withStdin(t, "", func() {
			out = captureStdout(t, func() {
				if code := runInit([]string{"--keep-board"}); code != 0 {
					t.Fatalf("exit = %d", code)
				}
			})
		})
	})
	if !strings.Contains(out, "SIDECAR.md is tracked — run 'git rm --cached SIDECAR.md' to untrack it.") {
		t.Errorf("missing untrack hint:\n%s", out)
	}
	// Round 4, AA1: the entry is still written — silently, since it's a
	// no-op while the file stays tracked but takes effect the instant the
	// human runs the untrack command above.
	if strings.Contains(out, `Added "SIDECAR.md"`) {
		t.Errorf("printed a misleading exclude confirmation for a tracked file:\n%s", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if !strings.Contains(string(data), "SIDECAR.md") {
		t.Errorf("info/exclude missing SIDECAR.md — entry should be written (silently) even while tracked: %q", data)
	}
	// Round 3, Z1: the .sidecar/ snapshot dir that `sidecar diff` will
	// create beside the tracked board must still be excluded, even though
	// the board itself can't be — otherwise the first `sidecar diff` run
	// litters git status.
	if !strings.Contains(string(data), sidecarDirName+"/") {
		t.Errorf("info/exclude missing %s/ snapshot dir for a tracked legacy board: %q", sidecarDirName, data)
	}
}

// Round 4, AA2: the exclude entry (and the snapshot-dir entry) must be
// repo-relative, the same way gitExcludeDefault computes it — otherwise
// running --keep-board on a tracked legacy board from a repo subdirectory
// writes a bare "SIDECAR.md" / ".sidecar/" that doesn't match the file's
// actual repo-relative path.
func TestInitKeepBoardTrackedLegacyFromSubdirUsesRepoRelativePath(t *testing.T) {
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, "git", "add", "sub/SIDECAR.md")

	withWorkDir(t, sub, func() {
		withStdin(t, "", func() {
			captureStdout(t, func() {
				if code := runInit([]string{"--keep-board"}); code != 0 {
					t.Fatalf("exit = %d", code)
				}
			})
		})
	})
	data, _ := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if !strings.Contains(string(data), filepath.Join("sub", "SIDECAR.md")) {
		t.Errorf("info/exclude missing repo-relative sub/SIDECAR.md: %q", data)
	}
	if !strings.Contains(string(data), filepath.Join("sub", sidecarDirName)+"/") {
		t.Errorf("info/exclude missing repo-relative sub/%s/: %q", sidecarDirName, data)
	}
}

// Issue #16: the reconcile reminder uses bare `sidecar` (not the path) when
// rel is the default board location, and keeps the explicit path for a
// custom board. The sentinel phrase must survive verbatim either way.
func TestReconcileMessageBareSidecarForDefaultBoard(t *testing.T) {
	defRel := filepath.Join(sidecarDirName, "sidecar.md")
	msg := reconcileMessage(defRel, defaultSections())
	if !strings.Contains(msg, "the human watches with `sidecar`.") {
		t.Errorf("default board should use bare `sidecar`:\n%s", msg)
	}
	if !strings.Contains(msg, hookSentinel) {
		t.Errorf("reminder lost the sentinel:\n%s", msg)
	}

	custom := reconcileMessage("notes.md", defaultSections())
	if !strings.Contains(custom, "the human watches with `sidecar notes.md`.") {
		t.Errorf("custom board should keep the explicit path:\n%s", custom)
	}
	if !strings.Contains(custom, hookSentinel) {
		t.Errorf("reminder lost the sentinel:\n%s", custom)
	}
}

// A custom target path with --yes must not block on the git-exclude prompt —
// the flag, not TTY state, decides. It should take the same outcome as
// pressing Enter at the prompt: append the path to .git/info/exclude.
func TestRunInitYesCustomPathSkipsExcludePrompt(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	target := filepath.Join(dir, "notes.md")
	withWorkDir(t, dir, func() {
		if code := runInit([]string{target, "--yes"}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	if _, err := os.Stat(target); err != nil {
		t.Fatal("board not created")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(data), "notes.md") {
		t.Errorf("notes.md not in .git/info/exclude: %q, %v", data, err)
	}
	// F4: the .sidecar/ snapshot dir that `sidecar diff` will create beside
	// a custom board path must be excluded too, or it shows up in git status.
	if !strings.Contains(string(data), sidecarDirName+"/") {
		t.Errorf(".sidecar/ not excluded alongside a custom path: %q", data)
	}
}

// Two things suppress the viewer launch. --yes is the "don't be interactive"
// switch — a wrapper running `sidecar init --yes` under a pty would otherwise
// block in the alt screen until someone pressed q. And a run that printed an
// instruction keeps the shell, because the alt screen hides that instruction
// until the human quits.
func TestShouldOpenViewer(t *testing.T) {
	cases := []struct {
		interactive, assumeYes, needsAttention, want bool
	}{
		{true, false, false, true},   // bare `sidecar init` from a terminal
		{true, true, false, false},   // --yes keeps its non-interactive contract
		{false, false, false, false}, // piped or CI: no viewer to open
		{false, true, false, false},
		// init printed something to act on — the alt screen would bury it
		// until the human quit, so stay in the shell and print the hint.
		{true, false, true, false},
		{true, true, true, false},
	}
	for _, c := range cases {
		if got := shouldOpenViewer(c.interactive, c.assumeYes, c.needsAttention); got != c.want {
			t.Errorf("shouldOpenViewer(interactive=%v, assumeYes=%v, needsAttention=%v) = %v, want %v",
				c.interactive, c.assumeYes, c.needsAttention, got, c.want)
		}
	}
}

// runInitBoard hands main() a path only when it should launch. Help, a flag
// error, and --yes all return empty while still doing (or correctly skipping)
// the setup work.
func TestRunInitBoardOpenPath(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	withWorkDir(t, dir, func() {
		var code int
		var open string
		out := captureStdout(t, func() {
			code, open = runInitBoard([]string{"--yes", "--no-claude"})
		})
		if code != 0 {
			t.Fatalf("init exit = %d, want 0", code)
		}
		if open != "" {
			t.Errorf("--yes returned %q, want empty so main doesn't launch", open)
		}
		if !strings.Contains(out, "Watch it:") {
			t.Errorf("a non-launching run must print the hint instead:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(sidecarDirName, "sidecar.md")); err != nil {
			t.Errorf("board not created: %v", err)
		}

		captureStdout(t, func() {
			code, open = runInitBoard([]string{"-h"})
		})
		if code != 0 || open != "" {
			t.Errorf("help returned (%d, %q), want (0, \"\")", code, open)
		}
		captureStderr(t, func() {
			code, open = runInitBoard([]string{"--bogus"})
		})
		if code != 2 || open != "" {
			t.Errorf("unknown flag returned (%d, %q), want (2, \"\")", code, open)
		}
	})
}

// Renaming 🧠 Needs action to 🧠 Needs you means an existing board's heading no
// longer matches the label-keyed hint map, so init re-runs would emit that one
// section with no "— meaning" while its siblings kept theirs. The role lookup
// matches it by emoji, so the hint survives the rename.
func TestSectionsFromBoardRecoversHintAcrossRename(t *testing.T) {
	legacy := "# Sidecar\n\n## 🧠 Needs action\n\n- x\n\n## 🚧 In progress\n\n- y\n"
	got, ok := sectionsFromBoard(legacy)
	if !ok {
		t.Fatal("sectionsFromBoard rejected a legacy board")
	}
	if got[0].Hint != defaultSections()[0].Hint {
		t.Errorf("renamed section lost its hint: got %q, want %q", got[0].Hint, defaultSections()[0].Hint)
	}
	if got[1].Hint == "" {
		t.Errorf("unrenamed section lost its hint: %+v", got[1])
	}
	// A genuinely custom section must still come back hintless.
	custom, _ := sectionsFromBoard("## Todo\n\n- x\n")
	if custom[0].Hint != "" {
		t.Errorf("custom section invented a hint: %q", custom[0].Hint)
	}
}

// The launch decision is only as good as the reporting behind it: each writer
// has to say when it left the human an instruction rather than just printing
// one and returning.
func TestWritersReportNeedsAttention(t *testing.T) {
	t.Run("hook on invalid JSON", func(t *testing.T) {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
		os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte("{not json"), 0o644)
		var got bool
		captureStdout(t, func() { got = writeReconcileHook(root, "SIDECAR.md", defaultSections()) })
		if !got {
			t.Error("printed a paste-this snippet but reported nothing to attend to")
		}
	})
	t.Run("hook on a clean write", func(t *testing.T) {
		root := t.TempDir()
		var got bool
		captureStdout(t, func() { got = writeReconcileHook(root, "SIDECAR.md", defaultSections()) })
		if got {
			t.Error("a successful write must not hold back the viewer")
		}
	})
	t.Run("note with an unclosed marker", func(t *testing.T) {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, "CLAUDE.md"),
			[]byte("# P\n\n<!-- "+claudeNoteMarker+" -->\nstranded\n"), 0o644)
		var got bool
		captureStdout(t, func() { got = writeClaudeNote(root, "SIDECAR.md", defaultSections()) })
		if !got {
			t.Error("told the human to fix it by hand but reported nothing to attend to")
		}
	})
	t.Run("note on a clean append", func(t *testing.T) {
		root := t.TempDir()
		var got bool
		captureStdout(t, func() { got = writeClaudeNote(root, "SIDECAR.md", defaultSections()) })
		if got {
			t.Error("a successful append must not hold back the viewer")
		}
	})
}

// A symlinked board is refused outright. Boards are per-directory: a link
// points two checkouts at one file, and every session writing there piles
// into a single queue.
func TestRunInitRefusesSymlinkedBoardFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.md")
	if err := os.WriteFile(real, []byte("# Real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "board.md")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStderr(t, func() { code = runInit([]string{link}) })
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a symlinked board", code)
	}
	if !strings.Contains(out, "symlink") {
		t.Errorf("stderr = %q, want it to name the symlink", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("wrote CLAUDE.md after refusing")
	}
	data, _ := os.ReadFile(real)
	if string(data) != "# Real\n" {
		t.Errorf("wrote through the symlink: %q", data)
	}
}

// The .sidecar/ directory itself is the other half of the same mistake —
// linking the board's home shares every board inside it.
func TestRunInitRefusesSymlinkedSidecarDir(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(dir, sidecarDirName)); err != nil {
		t.Fatal(err)
	}

	var code int
	captureStderr(t, func() { code = runInit([]string{filepath.Join(dir, sidecarDirName, "sidecar.md")}) })
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a symlinked %s/", code, sidecarDirName)
	}
	if _, err := os.Stat(filepath.Join(shared, "sidecar.md")); !os.IsNotExist(err) {
		t.Error("scaffolded a board into the link's target")
	}
}

// A dangling link is the dangerous case: os.Stat fails, so without an Lstat
// check init falls through to scaffold and writes the board at the link's
// target instead of here.
func TestRunInitRefusesDanglingBoardSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "gone.md")
	if err := os.Symlink(gone, filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Fatal(err)
	}

	var code int
	captureStderr(t, func() { code = runInit([]string{filepath.Join(dir, sidecarDirName, "sidecar.md")}) })
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a dangling board symlink", code)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Error("scaffolded through the dangling link")
	}
}

// Migration renames a legacy root board into .sidecar/ — a symlink moves as
// a symlink, so the check has to run before the move, not after it.
func TestRunInitRefusesSymlinkedLegacyBoard(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	elsewhere := t.TempDir()
	real := filepath.Join(elsewhere, "sidecar.md")
	if err := os.WriteFile(real, []byte("# Shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, legacyFile)); err != nil {
		t.Fatal(err)
	}

	var code int
	withWorkDir(t, dir, func() {
		captureStderr(t, func() { code = runInit([]string{"--yes"}) })
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a symlinked legacy board", code)
	}
	if fi, err := os.Lstat(filepath.Join(dir, legacyFile)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("legacy symlink moved: %v, %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); !os.IsNotExist(err) {
		t.Error("migrated the symlink into .sidecar/")
	}
}

// A real board reached through a symlinked parent — a project directory
// behind a link — is ordinary and must still init. Only the board and its
// own .sidecar/ home are checked.
func TestRunInitAllowsBoardUnderSymlinkedParent(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}

	if code := runInit([]string{filepath.Join(link, "notes.md")}); code != 0 {
		t.Fatalf("exit = %d, want 0 for a real board under a symlinked parent", code)
	}
	if _, err := os.Stat(filepath.Join(project, "notes.md")); err != nil {
		t.Errorf("board not created: %v", err)
	}
}

// The CLAUDE.md note carries the rule, so an agent knows not to create the
// symlink init refuses.
func TestClaudeNoteCarriesPerDirectoryRule(t *testing.T) {
	note := claudeNote(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	for _, want := range []string{"Never symlink", "sidecar init"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
}

// The viewer's create prompt scaffolds too, and a dangling link is the one
// symlinked board that reaches it — refuse there rather than write the board
// into the link's target directory.
func TestOfferCreateRefusesDanglingBoardSymlink(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "elsewhere", "sidecar.md")
	link := filepath.Join(dir, "board.md")
	if err := os.Symlink(gone, link); err != nil {
		t.Fatal(err)
	}

	out := captureStderr(t, func() { offerCreate(link) })
	if !strings.Contains(out, "symlink") {
		t.Errorf("stderr = %q, want the symlink refusal", out)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Error("scaffolded through the dangling link")
	}
}
