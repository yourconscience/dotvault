package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDocumentationMatchesImplementedCLIBehavior(t *testing.T) {
	root := repoRoot(t)
	readme := readText(t, filepath.Join(root, "README.md"))
	vaultSpec := readText(t, filepath.Join(root, "spec", "VAULT.md"))

	for _, stale := range []string{
		"placeholder executable",
		"planned for later implementation",
		"minimal Go scaffolding for later CLI work",
	} {
		if strings.Contains(readme, stale) {
			t.Fatalf("README still describes stale CLI state %q:\n%s", stale, readme)
		}
	}

	for _, want := range []string{
		"dotvault init",
		"dotvault import",
		"dotvault export",
		"dotvault sync",
		"dry-run",
		"DOTVAULT_PATH",
		"KNOWLEDGE_",
		"local bare",
		"no network services",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing implemented behavior %q:\n%s", want, readme)
		}
	}

	for _, args := range [][]string{
		{"--help"},
		{"init", "--help"},
		{"import", "--help"},
		{"export", "--help"},
		{"sync", "--help"},
	} {
		code, stdout, stderr := runTestCLI(t, args, nil)
		if code != 0 {
			t.Fatalf("help %v exit code = %d, stderr = %q", args, code, stderr)
		}
		if !strings.Contains(stdout, "Safety:") {
			t.Fatalf("help %v does not document safety defaults:\n%s", args, stdout)
		}
	}

	if !strings.Contains(vaultSpec, "memory/") || !strings.Contains(vaultSpec, "legacy knowledge-vault `ai/`") {
		t.Fatalf("vault spec no longer documents ai/ to memory/ migration:\n%s", vaultSpec)
	}
}

func TestPublicRepoPrivacyBoundaries(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"100" + ".73.153.20",
		"/srv/git/" + "knowledge.git",
		".claude/" + "projects",
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = filepath.Clean(home)
		if home != "." && home != string(os.PathSeparator) {
			forbidden = append(forbidden,
				home,
				filepath.Join(home, "Workspace", "knowledge"),
				filepath.Join(home, "Workspace", "vault"),
			)
		}
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".venv", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "dotvault" || rel == "uv.lock" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, needle := range forbidden {
			if strings.Contains(string(content), needle) {
				t.Fatalf("public repo file %s contains prohibited private identifier %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("privacy scan failed: %v", err)
	}
}

func TestCLITestHarnessUsesIsolatedHomeAndControlledEnv(t *testing.T) {
	env := controlledCLIEnv(t, map[string]string{"DOTVAULT_PATH": "~/vault"})
	if _, ok := env["KNOWLEDGE_REPO"]; ok {
		t.Fatalf("controlled env leaked legacy repository setting: %#v", env)
	}
	hostHome, err := os.UserHomeDir()
	if err == nil && env["HOME"] == hostHome {
		t.Fatalf("test HOME was not isolated from host home %s", hostHome)
	}

	code, stdout, stderr := runWithEnv([]string{"init", "--link-home"}, env)
	if code != 0 {
		t.Fatalf("init with controlled HOME exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	vault := filepath.Join(env["HOME"], "vault")
	assertVaultLayout(t, vault)
	link := filepath.Join(env["HOME"], ".vault")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("read isolated HOME/.vault: %v", err)
	}
	if target != vault {
		t.Fatalf("isolated HOME/.vault target = %q, want %q", target, vault)
	}
}

func TestSyncE2EUsesLocalBareFilesystemRemote(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	vault := seedVaultFromLocalBareRemote(t, root, remote)

	info, err := os.Stat(remote)
	if err != nil {
		t.Fatalf("stat local bare remote: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("remote %s is not a directory", remote)
	}
	remoteURL := strings.TrimSpace(gitOutput(t, vault, "remote", "get-url", "origin"))
	if remoteURL != remote {
		t.Fatalf("vault remote URL = %q, want local bare path %q", remoteURL, remote)
	}
	for _, networkMarker := range []string{"://", "git@", "100" + ".73.153.20"} {
		if strings.Contains(remoteURL, networkMarker) {
			t.Fatalf("sync e2e remote is not local-only: %q", remoteURL)
		}
	}
}

func runWithEnv(args []string, env map[string]string) (int, string, string) {
	var stdout, stderr strings.Builder
	code := run(args, func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readText(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
