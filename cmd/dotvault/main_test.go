package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runTestCLI(args []string, env map[string]string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTopLevelHelpListsCommands(t *testing.T) {
	code, stdout, stderr := runTestCLI([]string{"--help"}, nil)
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
			code, stdout, stderr := runTestCLI(tt.args, nil)
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

	code, stdout, stderr := runTestCLI([]string{"init", "--vault", vault}, env)
	if code != 0 {
		t.Fatalf("init exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	assertVaultLayout(t, vault)
	configPath := filepath.Join(vault, ".dotvault", "config.json")
	firstConfig := readConfig(t, configPath)
	if firstConfig["vaultPath"] != mustAbs(t, vault) {
		t.Fatalf("vaultPath = %#v, want %q", firstConfig["vaultPath"], mustAbs(t, vault))
	}

	code, stdout, stderr = runTestCLI([]string{"init", "--vault", vault}, env)
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

	code, stdout, stderr := runTestCLI([]string{"init"}, env)
	if code != 0 {
		t.Fatalf("init exit code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	assertVaultLayout(t, vault)
}

func TestInitDoesNotCreateHomeSymlinkByDefault(t *testing.T) {
	home := t.TempDir()
	vault := filepath.Join(t.TempDir(), "vault")

	code, stdout, stderr := runTestCLI([]string{"init", "--vault", vault}, map[string]string{"HOME": home})
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

	code, stdout, stderr := runTestCLI([]string{"init", "--vault", vault, "--link-home"}, env)
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

	code, stdout, stderr = runTestCLI([]string{"init", "--vault", vault, "--link-home"}, env)
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

		code, _, stderr := runTestCLI([]string{"init", "--vault", vault, "--link-home"}, map[string]string{"HOME": home})
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

		code, _, stderr := runTestCLI([]string{"init", "--vault", vault, "--link-home"}, map[string]string{"HOME": home})
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
