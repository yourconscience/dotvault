package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var vaultDirectories = []string{"notes", "memory", "profile", "sessions"}

type envLookup func(string) (string, bool)

type localConfig struct {
	SchemaVersion int        `json:"schemaVersion"`
	VaultPath     string     `json:"vaultPath"`
	Directories   []string   `json:"directories"`
	Sync          syncConfig `json:"sync"`
}

type syncConfig struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

func main() {
	os.Exit(run(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}

func run(args []string, getenv envLookup, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printTopLevelHelp(stdout)
		return 0
	}

	if args[0] == "help" {
		if len(args) == 1 {
			printTopLevelHelp(stdout)
			return 0
		}
		return run(append([]string{args[1], "--help"}, args[2:]...), getenv, stdout, stderr)
	}

	switch args[0] {
	case "init":
		return runInit(args[1:], getenv, stdout, stderr)
	case "import":
		return runPlannedCommand("import", args[1:], printImportHelp, stdout, stderr)
	case "export":
		return runPlannedCommand("export", args[1:], printExportHelp, stdout, stderr)
	case "sync":
		return runPlannedCommand("sync", args[1:], printSyncHelp, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "dotvault: unknown command %q\n\n", args[0])
		printTopLevelHelp(stderr)
		return 2
	}
}

func runInit(args []string, getenv envLookup, stdout, stderr io.Writer) int {
	var vaultFlag string
	var linkHome bool
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&vaultFlag, "vault", "", "path to the private vault to create; defaults to DOTVAULT_PATH when set")
	fs.BoolVar(&linkHome, "link-home", false, "opt in to creating HOME/.vault as a symlink to the selected vault")
	fs.Usage = func() {
		printInitHelp(stdout)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "dotvault init: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	vaultPath, err := resolveVaultPath(vaultFlag, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault init: %v\n", err)
		return 2
	}

	if err := initializeVault(vaultPath); err != nil {
		fmt.Fprintf(stderr, "dotvault init: %v\n", err)
		return 1
	}
	configPath, wroteConfig, err := writeLocalConfig(vaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault init: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Initialized dotvault vault: %s\n", vaultPath)
	fmt.Fprintf(stdout, "Ensured directories: %s\n", strings.Join(vaultDirectories, ", "))
	if wroteConfig {
		fmt.Fprintf(stdout, "Wrote local config: %s\n", configPath)
	} else {
		fmt.Fprintf(stdout, "Local config already safe: %s\n", configPath)
	}

	if linkHome {
		linkPath, created, err := ensureHomeVaultSymlink(vaultPath, getenv)
		if err != nil {
			fmt.Fprintf(stderr, "dotvault init: %v\n", err)
			return 1
		}
		if created {
			fmt.Fprintf(stdout, "Created home symlink: %s -> %s\n", linkPath, vaultPath)
		} else {
			fmt.Fprintf(stdout, "Home symlink already safe: %s -> %s\n", linkPath, vaultPath)
		}
	} else {
		fmt.Fprintln(stdout, "Home symlink not requested; pass --link-home to create HOME/.vault safely.")
	}

	return 0
}

func runPlannedCommand(name string, args []string, printHelp func(io.Writer), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printHelp(stdout)
	}

	switch name {
	case "import":
		fs.String("from", "", "knowledge-style source path")
		fs.String("vault", "", "dotvault destination path")
		fs.Bool("apply", false, "apply the planned migration; default is dry-run")
	case "export":
		fs.String("vault", "", "dotvault source path")
		fs.String("out", "", "safe output directory")
	case "sync":
		fs.String("vault", "", "dotvault git repository path")
		fs.String("remote", "", "git remote name")
		fs.String("branch", "", "git branch name")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(args) == 0 || (len(args) == 1 && isHelpArg(args[0])) {
		printHelp(stdout)
		return 0
	}

	fmt.Fprintf(stderr, "dotvault %s is planned for a later milestone; use --help for the safety contract.\n", name)
	return 2
}

func initializeVault(vaultPath string) error {
	if err := os.MkdirAll(vaultPath, 0o700); err != nil {
		return fmt.Errorf("create vault root: %w", err)
	}
	for _, dir := range vaultDirectories {
		if err := os.MkdirAll(filepath.Join(vaultPath, dir), 0o700); err != nil {
			return fmt.Errorf("create %s/: %w", dir, err)
		}
	}
	return nil
}

func writeLocalConfig(vaultPath string) (string, bool, error) {
	configDir := filepath.Join(vaultPath, ".dotvault")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", false, fmt.Errorf("create local config directory: %w", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	config := localConfig{
		SchemaVersion: 1,
		VaultPath:     vaultPath,
		Directories:   append([]string(nil), vaultDirectories...),
		Sync: syncConfig{
			Remote: "origin",
			Branch: "main",
		},
	}

	if existing, err := os.ReadFile(configPath); err == nil {
		var parsed localConfig
		if err := json.Unmarshal(existing, &parsed); err != nil {
			return configPath, false, fmt.Errorf("refusing to overwrite invalid existing config %s: %w", configPath, err)
		}
		if parsed.VaultPath != vaultPath {
			return configPath, false, fmt.Errorf("refusing to overwrite config for %s with %s", parsed.VaultPath, vaultPath)
		}
		return configPath, false, nil
	} else if !os.IsNotExist(err) {
		return configPath, false, fmt.Errorf("read local config: %w", err)
	}

	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return configPath, false, fmt.Errorf("encode local config: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		return configPath, false, fmt.Errorf("write local config: %w", err)
	}
	return configPath, true, nil
}

func ensureHomeVaultSymlink(vaultPath string, getenv envLookup) (string, bool, error) {
	home, ok := getenv("HOME")
	if !ok || strings.TrimSpace(home) == "" {
		return "", false, errors.New("--link-home requires HOME to be set")
	}
	homePath, err := expandAndAbs(home, getenv)
	if err != nil {
		return "", false, fmt.Errorf("resolve HOME: %w", err)
	}
	linkPath := filepath.Join(homePath, ".vault")

	info, err := os.Lstat(linkPath)
	if os.IsNotExist(err) {
		if err := os.Symlink(vaultPath, linkPath); err != nil {
			return linkPath, false, fmt.Errorf("create %s symlink: %w", linkPath, err)
		}
		return linkPath, true, nil
	}
	if err != nil {
		return linkPath, false, fmt.Errorf("inspect %s: %w", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return linkPath, false, fmt.Errorf("refusing to overwrite existing non-symlink %s", linkPath)
	}

	target, err := os.Readlink(linkPath)
	if err != nil {
		return linkPath, false, fmt.Errorf("read %s symlink: %w", linkPath, err)
	}
	targetAbs, err := symlinkTargetAbs(linkPath, target)
	if err != nil {
		return linkPath, false, fmt.Errorf("resolve existing %s symlink: %w", linkPath, err)
	}
	if targetAbs != vaultPath {
		return linkPath, false, fmt.Errorf("refusing to overwrite existing %s symlink to %s", linkPath, targetAbs)
	}
	return linkPath, false, nil
}

func resolveVaultPath(explicit string, getenv envLookup) (string, error) {
	vaultPath := strings.TrimSpace(explicit)
	if vaultPath == "" {
		if envPath, ok := getenv("DOTVAULT_PATH"); ok {
			vaultPath = strings.TrimSpace(envPath)
		}
	}
	if vaultPath == "" {
		return "", errors.New("missing vault path; pass --vault <path> or set DOTVAULT_PATH")
	}
	return expandAndAbs(vaultPath, getenv)
}

func expandAndAbs(path string, getenv envLookup) (string, error) {
	expanded := path
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, ok := getenv("HOME")
		if !ok || strings.TrimSpace(home) == "" {
			return "", errors.New("HOME is required to expand ~")
		}
		if path == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func symlinkTargetAbs(linkPath, target string) (string, error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func printTopLevelHelp(w io.Writer) {
	fmt.Fprint(w, `dotvault manages a private vault using public, privacy-first tooling.

Usage:
  dotvault <command> [flags]
  dotvault help <command>

Commands:
  init     Create the standard vault layout and safe local config
  import   Plan a dry-run-first migration from a knowledge-style vault
  export   Export public/template-safe content only
  sync     Inspect or synchronize a private vault git repository

Environment:
  DOTVAULT_PATH selects the private vault path when --vault is omitted.
  DOTVAULT_REMOTE and DOTVAULT_BRANCH are used by sync in later milestones.
  KNOWLEDGE_* variables are legacy fallbacks for migration compatibility.

Safety:
  Commands default to local filesystem fixtures and conservative behavior.
  Destructive operations require explicit opt-in flags and must not touch live private paths during tests.
`)
}

func printInitHelp(w io.Writer) {
	fmt.Fprint(w, `dotvault init creates a private vault layout safely.

Usage:
  dotvault init --vault <path> [--link-home]

Flags:
  --vault <path>   Private vault path to create. Default: DOTVAULT_PATH if set; otherwise required.
  --link-home      Opt in to creating HOME/.vault as a symlink to the vault.
  -h, --help       Show this help.

Creates:
  notes/, memory/, profile/, sessions/
  .dotvault/config.json with schemaVersion, vaultPath, directory nouns, and sync defaults.

Environment:
  DOTVAULT_PATH may provide the vault path when --vault is omitted.
  HOME is used only with --link-home to locate ~/.vault.

Safety:
  Init is idempotent for existing vault directories and matching local config.
  Symlink handling is opt-in and never overwrites an existing unsafe ~/.vault path or non-matching symlink.
`)
}

func printImportHelp(w io.Writer) {
	fmt.Fprint(w, `dotvault import migrates a knowledge-style vault into dotvault format.

Usage:
  dotvault import --from <knowledge-path> --vault <vault-path> [--apply]

Flags:
  --from <path>    Source knowledge-style vault. Required.
  --vault <path>   Destination dotvault path. Default: DOTVAULT_PATH if set.
  --apply          Apply the migration. Default: dry-run/status output only.
  -h, --help       Show this help.

Environment:
  DOTVAULT_PATH may provide the destination vault path.
  KNOWLEDGE_DIR or KNOWLEDGE_REPO may provide a legacy source path in later milestones.

Safety:
  Default behavior is dry-run and preserves the source without filesystem changes.
  Real import maps ai/ to memory/ and refuses unsafe source or destination states.
`)
}

func printExportHelp(w io.Writer) {
	fmt.Fprint(w, `dotvault export writes public/template-safe content.

Usage:
  dotvault export --vault <path> --out <path>

Flags:
  --vault <path>   Source dotvault path. Default: DOTVAULT_PATH if set.
  --out <path>     Output directory for public/template content. Required.
  -h, --help       Show this help.

Environment:
  DOTVAULT_PATH may provide the private vault path.

Safety:
  Export defaults to public/template content only and does not copy private notes, memory, profile, or sessions.
  Export refuses unsafe output targets, non-empty destinations, and symlink escapes unless a future explicit safe force mode is implemented.
`)
}

func printSyncHelp(w io.Writer) {
	fmt.Fprint(w, `dotvault sync wraps safe git synchronization for a private vault.

Usage:
  dotvault sync <status|pull|push|run> [flags]

Subcommands:
  status  Inspect git state without mutation.
  pull    Fetch and rebase configured remote changes.
  push    Push committed local changes.
  run     Stage allowed vault paths, commit, pull, and push with locking.

Flags:
  --vault <path>    Vault git repository path. Default: DOTVAULT_PATH or legacy KNOWLEDGE_DIR/KNOWLEDGE_REPO.
  --remote <name>   Git remote. Default: DOTVAULT_REMOTE or legacy KNOWLEDGE_REMOTE, then origin.
  --branch <name>   Git branch. Default: DOTVAULT_BRANCH or legacy KNOWLEDGE_BRANCH, then main.
  -h, --help        Show this help.

Safety:
  Sync uses .git/dotvault-sync.lock, refuses unsafe dirty/conflict states, and never uses blind git add -A.
  DOTVAULT_ environment variables take precedence; KNOWLEDGE_ variables are compatibility fallbacks.
`)
}
