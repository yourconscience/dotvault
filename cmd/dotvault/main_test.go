package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func runTestCLI(t testing.TB, args []string, env map[string]string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	controlledEnv := controlledCLIEnv(t, env)
	code := run(args, func(key string) (string, bool) {
		value, ok := controlledEnv[key]
		return value, ok
	}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func controlledCLIEnv(t testing.TB, env map[string]string) map[string]string {
	t.Helper()
	controlledEnv := map[string]string{
		"HOME":   t.TempDir(),
		"TMPDIR": t.TempDir(),
	}
	for key, value := range env {
		controlledEnv[key] = value
	}
	return controlledEnv
}

func TestTopLevelHelpListsCommands(t *testing.T) {
	code, stdout, stderr := runTestCLI(t, []string{"--help"}, nil)
	if code != 0 {
		t.Fatalf("help exit code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"init", "import", "export", "sync"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("top-level help missing %q:\n%s", want, stdout)
		}
	}
}

func TestSubcommandHelpDocumentsFlagsDefaultsAndSafety(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "init",
			args: []string{"init", "--help"},
			want: []string{"--vault", "DOTVAULT_PATH", "--link-home", "HOME", "~/.vault", "never overwrites"},
		},
		{
			name: "import",
			args: []string{"import", "--help"},
			want: []string{"--from", "--vault", "dry-run", "DOTVAULT_PATH", "preserves the source"},
		},
		{
			name: "export",
			args: []string{"export", "--help"},
			want: []string{"--vault", "--out", "public/template", "refuses unsafe"},
		},
		{
			name: "sync",
			args: []string{"sync", "--help"},
			want: []string{"status", "pull", "push", "run", "DOTVAULT_", "KNOWLEDGE_", ".git/dotvault-sync.lock"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runTestCLI(t, tt.args, nil)
			if code != 0 {
				t.Fatalf("%v exit code = %d, stderr = %q", tt.args, code, stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("%v help missing %q:\n%s", tt.args, want, stdout)
				}
			}
		})
	}
}

func TestInitCreatesVaultLayoutAndConfigIdempotently(t *testing.T) {
	home := t.TempDir()
	vault := filepath.Join(t.TempDir(), "vault")
	env := map[string]string{"HOME": home}

	code, stdout, stderr := runTestCLI(t, []string{"init", "--vault", vault}, env)
	if code != 0 {
		t.Fatalf("init exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	assertVaultLayout(t, vault)
	configPath := filepath.Join(vault, ".dotvault", "config.json")
	firstConfig := readConfig(t, configPath)
	if firstConfig["vaultPath"] != mustAbs(t, vault) {
		t.Fatalf("vaultPath = %#v, want %q", firstConfig["vaultPath"], mustAbs(t, vault))
	}

	code, stdout, stderr = runTestCLI(t, []string{"init", "--vault", vault}, env)
	if code != 0 {
		t.Fatalf("second init exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	assertVaultLayout(t, vault)
	secondConfig := readConfig(t, configPath)
	if first, second := asJSON(t, firstConfig), asJSON(t, secondConfig); first != second {
		t.Fatalf("config changed on idempotent rerun:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestInitUsesDOTVAULTPathEnvironment(t *testing.T) {
	home := t.TempDir()
	vault := filepath.Join(t.TempDir(), "env-vault")
	env := map[string]string{
		"HOME":          home,
		"DOTVAULT_PATH": vault,
	}

	code, stdout, stderr := runTestCLI(t, []string{"init"}, env)
	if code != 0 {
		t.Fatalf("init exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	assertVaultLayout(t, vault)
}

func TestInitDoesNotCreateHomeSymlinkByDefault(t *testing.T) {
	home := t.TempDir()
	vault := filepath.Join(t.TempDir(), "vault")

	code, stdout, stderr := runTestCLI(t, []string{"init", "--vault", vault}, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("init exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(home, ".vault")); !os.IsNotExist(err) {
		t.Fatalf("default init created HOME/.vault or hit unexpected error: %v", err)
	}
}

func TestInitCreatesHomeSymlinkOnlyWhenSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on many Windows systems")
	}
	home := t.TempDir()
	vault := filepath.Join(t.TempDir(), "vault")
	env := map[string]string{"HOME": home}

	code, stdout, stderr := runTestCLI(t, []string{"init", "--vault", vault, "--link-home"}, env)
	if code != 0 {
		t.Fatalf("init --link-home exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	linkPath := filepath.Join(home, ".vault")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat HOME/.vault: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("HOME/.vault mode = %v, want symlink", info.Mode())
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink HOME/.vault: %v", err)
	}
	if target != mustAbs(t, vault) {
		t.Fatalf("HOME/.vault target = %q, want %q", target, mustAbs(t, vault))
	}

	code, stdout, stderr = runTestCLI(t, []string{"init", "--vault", vault, "--link-home"}, env)
	if code != 0 {
		t.Fatalf("idempotent init --link-home exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestInitRefusesUnsafeExistingHomeVault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}
	t.Run("existing file", func(t *testing.T) {
		home := t.TempDir()
		vault := filepath.Join(t.TempDir(), "vault")
		linkPath := filepath.Join(home, ".vault")
		if err := os.WriteFile(linkPath, []byte("do not replace"), 0o600); err != nil {
			t.Fatalf("write existing HOME/.vault: %v", err)
		}

		code, _, stderr := runTestCLI(t, []string{"init", "--vault", vault, "--link-home"}, map[string]string{"HOME": home})
		if code == 0 {
			t.Fatalf("init --link-home unexpectedly succeeded with existing file")
		}
		content, err := os.ReadFile(linkPath)
		if err != nil {
			t.Fatalf("read existing HOME/.vault: %v", err)
		}
		if string(content) != "do not replace" {
			t.Fatalf("existing HOME/.vault was overwritten; stderr = %q", stderr)
		}
	})

	t.Run("existing symlink to different target", func(t *testing.T) {
		home := t.TempDir()
		vault := filepath.Join(t.TempDir(), "vault")
		other := filepath.Join(t.TempDir(), "other-vault")
		if err := os.MkdirAll(other, 0o700); err != nil {
			t.Fatalf("mkdir other vault: %v", err)
		}
		linkPath := filepath.Join(home, ".vault")
		if err := os.Symlink(other, linkPath); err != nil {
			t.Fatalf("create existing HOME/.vault symlink: %v", err)
		}

		code, _, stderr := runTestCLI(t, []string{"init", "--vault", vault, "--link-home"}, map[string]string{"HOME": home})
		if code == 0 {
			t.Fatalf("init --link-home unexpectedly succeeded with mismatched symlink")
		}
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink existing HOME/.vault: %v", err)
		}
		if target != other {
			t.Fatalf("existing HOME/.vault target changed to %q; stderr = %q", target, stderr)
		}
	})
}

func TestImportDryRunDoesNotMutateDestinationOrSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "knowledge")
	vault := filepath.Join(root, "vault")
	createKnowledgeFixture(t, source)
	beforeSource := snapshotTree(t, source)

	code, stdout, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("import dry-run exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, want := range []string{"Dry-run import plan", "ai/ -> memory/", "notes/", "profile/", "sessions/", "no filesystem changes"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Lstat(vault); !os.IsNotExist(err) {
		t.Fatalf("dry-run created destination or hit unexpected error: %v", err)
	}
	if after := snapshotTree(t, source); beforeSource != after {
		t.Fatalf("source changed during dry-run:\nbefore=%s\nafter=%s", beforeSource, after)
	}
}

func TestImportApplyMapsAIToMemoryAndPreservesSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "knowledge")
	vault := filepath.Join(root, "vault")
	createKnowledgeFixture(t, source)
	beforeSource := snapshotTree(t, source)

	code, stdout, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("import --apply exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Applied import") {
		t.Fatalf("apply output did not report success:\n%s", stdout)
	}

	assertFileContent(t, filepath.Join(vault, "memory", "agent.md"), "PRIVATE_FIXTURE_AI_MEMORY\n")
	assertFileContent(t, filepath.Join(vault, "notes", "note.md"), "PRIVATE_FIXTURE_NOTE\n")
	assertFileContent(t, filepath.Join(vault, "profile", "profile.md"), "PRIVATE_FIXTURE_PROFILE\n")
	assertFileContent(t, filepath.Join(vault, "sessions", "session.md"), "PRIVATE_FIXTURE_SESSION\n")
	if _, err := os.Lstat(filepath.Join(vault, "ai")); !os.IsNotExist(err) {
		t.Fatalf("import created destination ai/ or hit unexpected error: %v", err)
	}
	if after := snapshotTree(t, source); beforeSource != after {
		t.Fatalf("source changed during apply:\nbefore=%s\nafter=%s", beforeSource, after)
	}
}

func TestImportRefusesUnsafeStatesWithoutPartialDestination(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		root := t.TempDir()
		vault := filepath.Join(root, "vault")
		code, _, stderr := runTestCLI(t, []string{"import", "--from", filepath.Join(root, "missing"), "--vault", vault, "--apply"}, nil)
		if code == 0 {
			t.Fatalf("import unexpectedly succeeded with missing source")
		}
		if _, err := os.Lstat(vault); !os.IsNotExist(err) {
			t.Fatalf("missing-source import created destination or hit unexpected error: %v; stderr=%q", err, stderr)
		}
	})

	t.Run("prepopulated destination", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "knowledge")
		vault := filepath.Join(root, "vault")
		createKnowledgeFixture(t, source)
		if err := os.MkdirAll(filepath.Join(vault, "notes"), 0o700); err != nil {
			t.Fatalf("mkdir destination: %v", err)
		}
		existing := filepath.Join(vault, "notes", "existing.md")
		if err := os.WriteFile(existing, []byte("keep me\n"), 0o600); err != nil {
			t.Fatalf("write existing destination file: %v", err)
		}

		code, _, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
		if code == 0 {
			t.Fatalf("import unexpectedly succeeded with prepopulated destination")
		}
		assertFileContent(t, existing, "keep me\n")
		if _, err := os.Lstat(filepath.Join(vault, "memory")); !os.IsNotExist(err) {
			t.Fatalf("failed import left partial memory/ directory or hit unexpected error: %v; stderr=%q", err, stderr)
		}
	})

	t.Run("dirty git source", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "knowledge")
		vault := filepath.Join(root, "vault")
		createKnowledgeFixture(t, source)
		runGit(t, source, "init")
		runGit(t, source, "config", "user.name", "Fixture")
		runGit(t, source, "config", "user.email", "fixture@example.invalid")
		runGit(t, source, "add", ".")
		runGit(t, source, "commit", "-m", "fixture")
		if err := os.WriteFile(filepath.Join(source, "notes", "dirty.md"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatalf("write dirty file: %v", err)
		}

		code, _, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
		if code == 0 {
			t.Fatalf("import unexpectedly succeeded with dirty git source")
		}
		if !strings.Contains(stderr, "dirty git source") {
			t.Fatalf("dirty git refusal stderr missing reason: %q", stderr)
		}
		if _, err := os.Lstat(vault); !os.IsNotExist(err) {
			t.Fatalf("dirty-source import created destination or hit unexpected error: %v", err)
		}
	})

	t.Run("non-regular source entry", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fifo behavior differs on Windows")
		}
		root := t.TempDir()
		source := filepath.Join(root, "knowledge")
		vault := filepath.Join(root, "vault")
		createKnowledgeFixture(t, source)
		fifo := filepath.Join(source, "notes", "pipe")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("mkfifo %s: %v", fifo, err)
		}

		code, _, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
		if code == 0 {
			t.Fatalf("import unexpectedly succeeded with non-regular source entry")
		}
		if !strings.Contains(stderr, "non-regular") {
			t.Fatalf("non-regular refusal missing reason: %q", stderr)
		}
		if _, err := os.Lstat(vault); !os.IsNotExist(err) {
			t.Fatalf("non-regular-source import created destination or hit unexpected error: %v", err)
		}
	})

	t.Run("idempotent rerun", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "knowledge")
		vault := filepath.Join(root, "vault")
		createKnowledgeFixture(t, source)
		code, stdout, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
		if code != 0 {
			t.Fatalf("first import --apply exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
		beforeVault := snapshotTree(t, vault)

		code, _, stderr = runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
		if code == 0 {
			t.Fatalf("idempotent rerun unexpectedly succeeded")
		}
		if after := snapshotTree(t, vault); beforeVault != after {
			t.Fatalf("failed rerun changed destination:\nbefore=%s\nafter=%s\nstderr=%s", beforeVault, after, stderr)
		}
	})
}

func TestImportRefusesDestinationInsideSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "knowledge")
	vault := filepath.Join(source, "vault")
	createKnowledgeFixture(t, source)
	beforeSource := snapshotTree(t, source)

	code, _, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
	if code == 0 {
		t.Fatalf("import unexpectedly succeeded with destination inside source")
	}
	if after := snapshotTree(t, source); beforeSource != after {
		t.Fatalf("destination-inside-source import changed source:\nbefore=%s\nafter=%s\nstderr=%s", beforeSource, after, stderr)
	}
}

func TestImportRefusesDestinationInsideSourceThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "knowledge")
	link := filepath.Join(root, "source-link")
	vault := filepath.Join(link, "vault")
	createKnowledgeFixture(t, source)
	if err := os.Symlink(source, link); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}
	beforeSource := snapshotTree(t, source)

	code, _, stderr := runTestCLI(t, []string{"import", "--from", source, "--vault", vault, "--apply"}, nil)
	if code == 0 {
		t.Fatalf("import unexpectedly succeeded with destination inside source through symlink")
	}
	if after := snapshotTree(t, source); beforeSource != after {
		t.Fatalf("symlink destination import changed source:\nbefore=%s\nafter=%s\nstderr=%s", beforeSource, after, stderr)
	}
}

func TestExportWritesTemplateOnlyAndExcludesPrivateSentinels(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	createVaultWithPrivateSentinels(t, vault)
	out := filepath.Join(root, "export")

	code, stdout, stderr := runTestCLI(t, []string{"export", "--vault", vault, "--out", out}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("export exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Exported public template") {
		t.Fatalf("export output did not report template export:\n%s", stdout)
	}
	for _, path := range []string{
		"README.md",
		"AGENTS.md",
		"notes/.gitkeep",
		"memory/.gitkeep",
		"profile/.gitkeep",
		"sessions/.gitkeep",
	} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Fatalf("export missing %s: %v", path, err)
		}
	}
	exported := snapshotTree(t, out)
	for _, forbidden := range []string{"PRIVATE_FIXTURE_NOTE", "PRIVATE_FIXTURE_MEMORY", "PRIVATE_FIXTURE_PROFILE", "PRIVATE_FIXTURE_SESSION", "PRIVATE_FIXTURE_CONFIG"} {
		if strings.Contains(exported, forbidden) {
			t.Fatalf("export leaked sentinel %q:\n%s", forbidden, exported)
		}
	}
}

func TestExportRefusesUnsafeOutputTargets(t *testing.T) {
	t.Run("non-empty output", func(t *testing.T) {
		root := t.TempDir()
		vault := filepath.Join(root, "vault")
		out := filepath.Join(root, "export")
		createVaultWithPrivateSentinels(t, vault)
		if err := os.MkdirAll(out, 0o700); err != nil {
			t.Fatalf("mkdir output: %v", err)
		}
		existing := filepath.Join(out, "existing.txt")
		if err := os.WriteFile(existing, []byte("keep\n"), 0o600); err != nil {
			t.Fatalf("write existing output file: %v", err)
		}

		code, _, stderr := runTestCLI(t, []string{"export", "--vault", vault, "--out", out}, nil)
		if code == 0 {
			t.Fatalf("export unexpectedly succeeded with non-empty output")
		}
		assertFileContent(t, existing, "keep\n")
		if !strings.Contains(stderr, "non-empty") {
			t.Fatalf("non-empty refusal missing reason: %q", stderr)
		}
	})

	t.Run("symlink output", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink behavior differs on Windows")
		}
		root := t.TempDir()
		vault := filepath.Join(root, "vault")
		createVaultWithPrivateSentinels(t, vault)
		out := filepath.Join(root, "export-link")
		if err := os.Symlink(filepath.Join(vault, "notes"), out); err != nil {
			t.Fatalf("create output symlink: %v", err)
		}

		code, _, stderr := runTestCLI(t, []string{"export", "--vault", vault, "--out", out}, nil)
		if code == 0 {
			t.Fatalf("export unexpectedly succeeded with symlink output")
		}
		if !strings.Contains(stderr, "symlink") {
			t.Fatalf("symlink refusal missing reason: %q", stderr)
		}
		assertFileContent(t, filepath.Join(vault, "notes", "secret.md"), "PRIVATE_FIXTURE_NOTE\n")
	})

	t.Run("output inside vault", func(t *testing.T) {
		root := t.TempDir()
		vault := filepath.Join(root, "vault")
		createVaultWithPrivateSentinels(t, vault)
		out := filepath.Join(vault, "notes", "export")

		code, _, stderr := runTestCLI(t, []string{"export", "--vault", vault, "--out", out}, nil)
		if code == 0 {
			t.Fatalf("export unexpectedly succeeded with output inside vault")
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatalf("unsafe export created output inside vault or hit unexpected error: %v; stderr=%q", err, stderr)
		}
	})
}

func assertVaultLayout(t *testing.T, vault string) {
	t.Helper()
	for _, dir := range []string{"notes", "memory", "profile", "sessions"} {
		info, err := os.Stat(filepath.Join(vault, dir))
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
}

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("unmarshal config %q: %v", content, err)
	}
	return config
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs %q: %v", path, err)
	}
	return abs
}

func asJSON(t *testing.T, value any) string {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(content)
}

func createKnowledgeFixture(t *testing.T, source string) {
	t.Helper()
	files := map[string]string{
		"ai/agent.md":          "PRIVATE_FIXTURE_AI_MEMORY\n",
		"notes/note.md":        "PRIVATE_FIXTURE_NOTE\n",
		"profile/profile.md":   "PRIVATE_FIXTURE_PROFILE\n",
		"sessions/session.md":  "PRIVATE_FIXTURE_SESSION\n",
		"notes/nested/deep.md": "PRIVATE_FIXTURE_NESTED_NOTE\n",
	}
	for rel, content := range files {
		writeFixtureFile(t, filepath.Join(source, rel), content)
	}
}

func createVaultWithPrivateSentinels(t *testing.T, vault string) {
	t.Helper()
	files := map[string]string{
		"notes/secret.md":     "PRIVATE_FIXTURE_NOTE\n",
		"memory/secret.md":    "PRIVATE_FIXTURE_MEMORY\n",
		"profile/secret.md":   "PRIVATE_FIXTURE_PROFILE\n",
		"sessions/secret.md":  "PRIVATE_FIXTURE_SESSION\n",
		".dotvault/config.js": "PRIVATE_FIXTURE_CONFIG\n",
	}
	for rel, content := range files {
		writeFixtureFile(t, filepath.Join(vault, rel), content)
	}
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			b.WriteString("L ")
			b.WriteString(filepath.ToSlash(rel))
			b.WriteString(" -> ")
			b.WriteString(target)
			b.WriteByte('\n')
		case d.IsDir():
			b.WriteString("D ")
			b.WriteString(filepath.ToSlash(rel))
			b.WriteByte('\n')
		default:
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			b.WriteString("F ")
			b.WriteString(filepath.ToSlash(rel))
			b.WriteString(" ")
			b.WriteString(info.Mode().String())
			b.WriteString(" ")
			b.Write(content)
			if !strings.HasSuffix(string(content), "\n") {
				b.WriteByte('\n')
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return b.String()
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, output)
	}
}
