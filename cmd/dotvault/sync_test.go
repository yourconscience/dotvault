package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncStatusReportsWithoutMutationAndResolvesLegacyEnv(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	vault := seedVaultFromLocalBareRemote(t, root, remote)

	before := gitOutput(t, vault, "status", "--porcelain")
	code, stdout, stderr := runTestCLI(t, []string{"sync", "status"}, map[string]string{
		"HOME":             t.TempDir(),
		"KNOWLEDGE_REPO":   vault,
		"KNOWLEDGE_REMOTE": "origin",
		"KNOWLEDGE_BRANCH": "main",
	})
	if code != 0 {
		t.Fatalf("sync status exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, want := range []string{"Vault:", vault, "Remote: origin", "Branch: main", "Status: clean"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("sync status output missing %q:\n%s", want, stdout)
		}
	}
	after := gitOutput(t, vault, "status", "--porcelain")
	if before != after {
		t.Fatalf("sync status mutated git state:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestSyncPrefersDOTVAULTEnvOverLegacyKnowledgeEnv(t *testing.T) {
	root := t.TempDir()
	preferredRemote := filepath.Join(root, "preferred.git")
	legacyRemote := filepath.Join(root, "legacy.git")
	preferred := seedVaultFromLocalBareRemote(t, filepath.Join(root, "preferred-root"), preferredRemote)
	legacy := seedVaultFromLocalBareRemote(t, filepath.Join(root, "legacy-root"), legacyRemote)

	code, stdout, stderr := runTestCLI(t, []string{"sync", "status"}, map[string]string{
		"HOME":            t.TempDir(),
		"DOTVAULT_PATH":   preferred,
		"DOTVAULT_REMOTE": "origin",
		"DOTVAULT_BRANCH": "main",
		"KNOWLEDGE_REPO":  legacy,
	})
	if code != 0 {
		t.Fatalf("sync status exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Vault: "+preferred) {
		t.Fatalf("sync did not prefer DOTVAULT_PATH over KNOWLEDGE_REPO:\n%s", stdout)
	}
	if strings.Contains(stdout, legacy) {
		t.Fatalf("sync status leaked legacy path despite DOTVAULT_PATH precedence:\n%s", stdout)
	}
}

func TestSyncPullPushAndRunWithLocalBareRemote(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	vault := seedVaultFromLocalBareRemote(t, root, remote)
	t.Logf("sync e2e local bare remote: %s", remote)

	remoteWork := cloneLocalBareRemote(t, root, remote, "remote-work")
	writeFixtureFile(t, filepath.Join(remoteWork, "notes", "remote.md"), "remote change\n")
	runGit(t, remoteWork, "add", "notes/remote.md")
	runGit(t, remoteWork, "commit", "-m", "remote change")
	runGit(t, remoteWork, "push", "origin", "main")

	code, stdout, stderr := runTestCLI(t, []string{"sync", "pull", "--vault", vault, "--remote", "origin", "--branch", "main"}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("sync pull exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Pulled origin/main") {
		t.Fatalf("sync pull did not report success:\n%s", stdout)
	}
	assertFileContent(t, filepath.Join(vault, "notes", "remote.md"), "remote change\n")

	writeFixtureFile(t, filepath.Join(vault, "notes", "local.md"), "local committed change\n")
	runGit(t, vault, "add", "notes/local.md")
	runGit(t, vault, "commit", "-m", "local committed change")
	writeFixtureFile(t, filepath.Join(vault, "scratch.txt"), "do not stage\n")
	code, stdout, stderr = runTestCLI(t, []string{"sync", "push", "--vault", vault, "--remote", "origin", "--branch", "main"}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("sync push exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Pushed HEAD to origin/main") {
		t.Fatalf("sync push did not report success:\n%s", stdout)
	}

	writeFixtureFile(t, filepath.Join(vault, "memory", "run.md"), "allowed run change\n")
	writeFixtureFile(t, filepath.Join(vault, "unrelated.txt"), "must stay untracked\n")
	code, stdout, stderr = runTestCLI(t, []string{"sync", "run", "--vault", vault, "--remote", "origin", "--branch", "main"}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("sync run exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, want := range []string{"Acquired sync lock", "Committed allowed vault changes", "Pushed HEAD to origin/main"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("sync run output missing %q:\n%s", want, stdout)
		}
	}
	status := gitOutput(t, vault, "status", "--porcelain")
	if !strings.Contains(status, "?? scratch.txt") || !strings.Contains(status, "?? unrelated.txt") {
		t.Fatalf("sync run did not preserve unrelated untracked files:\n%s", status)
	}
	if strings.Contains(status, "A  unrelated.txt") || strings.Contains(status, "A  scratch.txt") {
		t.Fatalf("sync run staged unrelated files:\n%s", status)
	}

	verificationClone := cloneLocalBareRemote(t, root, remote, "verification")
	assertFileContent(t, filepath.Join(verificationClone, "memory", "run.md"), "allowed run change\n")
}

func TestSyncRunUsesLockAndCleansUp(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	vault := seedVaultFromLocalBareRemote(t, root, remote)
	lockPath := filepath.Join(vault, ".git", "dotvault-sync.lock")
	if err := os.WriteFile(lockPath, []byte("held by test\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	code, _, stderr := runTestCLI(t, []string{"sync", "run", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
	if code == 0 {
		t.Fatalf("sync run unexpectedly succeeded while lock existed")
	}
	if !strings.Contains(stderr, ".git/dotvault-sync.lock") {
		t.Fatalf("lock refusal missing lock path: %q", stderr)
	}
	assertFileContent(t, lockPath, "held by test\n")

	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove test lock: %v", err)
	}
	code, stdout, stderr := runTestCLI(t, []string{"sync", "run", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("clean sync run exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("sync run left lock behind or hit unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "No allowed vault changes to commit") {
		t.Fatalf("clean sync run did not report no-op state:\n%s", stdout)
	}
}

func TestSyncRunRefusesPreStagedDisallowedPaths(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	vault := seedVaultFromLocalBareRemote(t, root, remote)

	writeFixtureFile(t, filepath.Join(vault, "memory", "allowed.md"), "allowed but not staged yet\n")
	writeFixtureFile(t, filepath.Join(vault, "scratch.txt"), "must not commit\n")
	runGit(t, vault, "add", "scratch.txt")

	code, _, stderr := runTestCLI(t, []string{"sync", "run", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
	if code == 0 {
		t.Fatalf("sync run unexpectedly succeeded with pre-staged disallowed path")
	}
	if !strings.Contains(stderr, "staged disallowed paths") || !strings.Contains(stderr, "scratch.txt") {
		t.Fatalf("disallowed staged refusal missing reason: %q", stderr)
	}
	status := gitOutput(t, vault, "status", "--porcelain")
	if !strings.Contains(status, "A  scratch.txt") {
		t.Fatalf("sync run did not preserve pre-staged disallowed path:\n%s", status)
	}
	if !strings.Contains(status, "?? memory/") && !strings.Contains(status, "?? memory/allowed.md") {
		t.Fatalf("sync run staged allowed changes before refusing disallowed staged file:\n%s", status)
	}
}

func TestSyncPushRefusesMissingAndNonFastForwardUpstream(t *testing.T) {
	t.Run("missing upstream", func(t *testing.T) {
		root := t.TempDir()
		remote := filepath.Join(root, "empty.git")
		runGit(t, root, "init", "--bare", remote)
		vault := filepath.Join(root, "vault")
		initializeGitVault(t, vault)
		runGit(t, vault, "remote", "add", "origin", remote)

		code, _, stderr := runTestCLI(t, []string{"sync", "push", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
		if code == 0 {
			t.Fatalf("sync push unexpectedly created a missing upstream")
		}
		if !strings.Contains(stderr, "missing upstream origin/main") {
			t.Fatalf("missing upstream refusal missing reason: %q", stderr)
		}
	})

	t.Run("non-fast-forward", func(t *testing.T) {
		root := t.TempDir()
		remote := filepath.Join(root, "remote.git")
		vault := seedVaultFromLocalBareRemote(t, root, remote)
		remoteWork := cloneLocalBareRemote(t, root, remote, "remote-work")
		writeFixtureFile(t, filepath.Join(remoteWork, "notes", "remote.md"), "remote ahead\n")
		runGit(t, remoteWork, "add", "notes/remote.md")
		runGit(t, remoteWork, "commit", "-m", "remote ahead")
		runGit(t, remoteWork, "push", "origin", "main")

		writeFixtureFile(t, filepath.Join(vault, "notes", "local.md"), "local diverged\n")
		runGit(t, vault, "add", "notes/local.md")
		runGit(t, vault, "commit", "-m", "local diverged")

		code, _, stderr := runTestCLI(t, []string{"sync", "push", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
		if code == 0 {
			t.Fatalf("sync push unexpectedly succeeded with non-fast-forward remote")
		}
		if !strings.Contains(stderr, "non-fast-forward") || !strings.Contains(stderr, "dotvault sync pull") {
			t.Fatalf("non-fast-forward refusal missing recovery guidance: %q", stderr)
		}
	})
}

func TestSyncRunConflictRecoveryDoesNotSpamBranches(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	vault := seedVaultFromLocalBareRemote(t, root, remote)
	remoteWork := cloneLocalBareRemote(t, root, remote, "remote-work")

	writeFixtureFile(t, filepath.Join(remoteWork, "notes", "conflict.md"), "remote conflict\n")
	runGit(t, remoteWork, "add", "notes/conflict.md")
	runGit(t, remoteWork, "commit", "-m", "remote conflict")
	runGit(t, remoteWork, "push", "origin", "main")

	writeFixtureFile(t, filepath.Join(vault, "notes", "conflict.md"), "local conflict\n")
	code, stdout, stderr := runTestCLI(t, []string{"sync", "run", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
	if code == 0 {
		t.Fatalf("sync run unexpectedly succeeded despite conflict; stdout=%q", stdout)
	}
	for _, want := range []string{"conflict", "Recovery commands:", "git -C", "rebase --continue"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("conflict output missing %q:\nstdout=%s\nstderr=%s", want, stdout, stderr)
		}
	}
	firstCount := countConflictBranches(t, vault)
	if firstCount != 1 {
		t.Fatalf("conflict branch count after first failure = %d, want 1\nbranches:\n%s", firstCount, gitOutput(t, vault, "branch", "--list"))
	}

	code, stdout, stderr = runTestCLI(t, []string{"sync", "run", "--vault", vault}, map[string]string{"HOME": t.TempDir()})
	if code == 0 {
		t.Fatalf("second sync run unexpectedly succeeded despite conflict; stdout=%q", stdout)
	}
	secondCount := countConflictBranches(t, vault)
	if secondCount != 1 {
		t.Fatalf("conflict branch count after repeat failure = %d, want 1\nbranches:\n%s\nstderr=%s", secondCount, gitOutput(t, vault, "branch", "--list"), stderr)
	}
}

func TestMigratedKnowledgeFixtureSyncsWithLegacyEnvAndLocalBareRemote(t *testing.T) {
	root := t.TempDir()
	knowledge := filepath.Join(root, "knowledge")
	vault := filepath.Join(root, "vault")
	remote := filepath.Join(root, "knowledge.git")
	createKnowledgeFixture(t, knowledge)
	runGit(t, root, "init", "--bare", remote)
	t.Logf("migrated knowledge sync uses local bare remote: %s", remote)

	code, _, stderr := runTestCLI(t, []string{"import", "--from", knowledge, "--vault", vault, "--apply"}, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("import --apply exit code = %d, stderr = %q", code, stderr)
	}
	initializeGitVault(t, vault)
	runGit(t, vault, "remote", "add", "origin", remote)
	runGit(t, vault, "push", "-u", "origin", "main")

	env := map[string]string{
		"HOME":             t.TempDir(),
		"KNOWLEDGE_REPO":   vault,
		"KNOWLEDGE_REMOTE": "origin",
		"KNOWLEDGE_BRANCH": "main",
	}
	code, stdout, stderr := runTestCLI(t, []string{"sync", "status"}, env)
	if code != 0 {
		t.Fatalf("legacy sync status exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Status: clean") {
		t.Fatalf("legacy sync status did not report clean migrated vault:\n%s", stdout)
	}
	code, stdout, stderr = runTestCLI(t, []string{"sync", "run"}, env)
	if code != 0 {
		t.Fatalf("legacy sync run exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "No allowed vault changes to commit") {
		t.Fatalf("legacy sync run did not no-op cleanly:\n%s", stdout)
	}
}

func seedVaultFromLocalBareRemote(t *testing.T, root, remote string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	seed := filepath.Join(root, "seed")
	initializeGitVault(t, seed)
	runGit(t, root, "init", "--bare", remote)
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "-u", "origin", "main")
	return cloneLocalBareRemote(t, root, remote, "vault")
}

func initializeGitVault(t *testing.T, path string) {
	t.Helper()
	for _, dir := range vaultDirectories {
		if err := os.MkdirAll(filepath.Join(path, dir), 0o700); err != nil {
			t.Fatalf("mkdir vault dir %s: %v", dir, err)
		}
	}
	writeFixtureFile(t, filepath.Join(path, "notes", "base.md"), "base\n")
	runGit(t, path, "init")
	runGit(t, path, "config", "user.name", "Fixture")
	runGit(t, path, "config", "user.email", "fixture@example.invalid")
	runGit(t, path, "checkout", "-B", "main")
	runGit(t, path, "add", ".")
	runGit(t, path, "commit", "-m", "base")
}

func cloneLocalBareRemote(t *testing.T, root, remote, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	runGit(t, root, "clone", remote, path)
	runGit(t, path, "config", "user.name", "Fixture")
	runGit(t, path, "config", "user.email", "fixture@example.invalid")
	runGit(t, path, "checkout", "main")
	return path
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitCommand(dir, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, output)
	}
	return string(output)
}

func countConflictBranches(t *testing.T, vault string) int {
	t.Helper()
	branches := strings.Split(gitOutput(t, vault, "branch", "--format=%(refname:short)", "--list", "dotvault-conflict-*"), "\n")
	count := 0
	for _, branch := range branches {
		if strings.TrimSpace(branch) != "" {
			count++
		}
	}
	return count
}
