package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var vaultDirectories = []string{"notes", "memory", "profile", "sessions"}

var importMappings = []struct {
	source string
	dest   string
}{
	{source: "ai", dest: "memory"},
	{source: "notes", dest: "notes"},
	{source: "profile", dest: "profile"},
	{source: "sessions", dest: "sessions"},
}

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
		return runImport(args[1:], getenv, stdout, stderr)
	case "export":
		return runExport(args[1:], getenv, stdout, stderr)
	case "sync":
		return runSync(args[1:], getenv, stdout, stderr)
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

func runImport(args []string, getenv envLookup, stdout, stderr io.Writer) int {
	var fromFlag string
	var vaultFlag string
	var apply bool
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&fromFlag, "from", "", "knowledge-style source path")
	fs.StringVar(&vaultFlag, "vault", "", "dotvault destination path")
	fs.BoolVar(&apply, "apply", false, "apply the planned migration; default is dry-run")
	fs.Usage = func() {
		printImportHelp(stdout)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "dotvault import: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	sourcePath, err := resolveImportSourcePath(fromFlag, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault import: %v\n", err)
		return 2
	}
	vaultPath, err := resolveVaultPath(vaultFlag, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault import: %v\n", err)
		return 2
	}

	plan, err := planImport(sourcePath, vaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault import: %v\n", err)
		return 1
	}
	if !apply {
		printImportPlan(stdout, plan)
		return 0
	}
	if err := applyImportPlan(plan); err != nil {
		fmt.Fprintf(stderr, "dotvault import: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Applied import from %s to %s\n", plan.sourcePath, plan.vaultPath)
	for _, item := range plan.items {
		fmt.Fprintf(stdout, "Copied %s/ -> %s/ (%d files)\n", item.sourceDir, item.destDir, item.fileCount)
	}
	return 0
}

func runExport(args []string, getenv envLookup, stdout, stderr io.Writer) int {
	var vaultFlag string
	var outFlag string
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&vaultFlag, "vault", "", "dotvault source path")
	fs.StringVar(&outFlag, "out", "", "safe output directory")
	fs.Usage = func() {
		printExportHelp(stdout)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "dotvault export: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	vaultPath, err := resolveVaultPath(vaultFlag, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault export: %v\n", err)
		return 2
	}
	outPath, err := resolveRequiredPath(outFlag, "--out", getenv)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault export: %v\n", err)
		return 2
	}
	if err := validateExportInputs(vaultPath, outPath); err != nil {
		fmt.Fprintf(stderr, "dotvault export: %v\n", err)
		return 1
	}
	if err := writeTemplateExport(outPath); err != nil {
		fmt.Fprintf(stderr, "dotvault export: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Exported public template content to %s\n", outPath)
	fmt.Fprintln(stdout, "Private notes, memory, profile, and sessions were not copied.")
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

type importPlan struct {
	sourcePath string
	vaultPath  string
	items      []importPlanItem
}

type importPlanItem struct {
	sourceDir string
	destDir   string
	fileCount int
}

func planImport(sourcePath, vaultPath string) (importPlan, error) {
	if err := validateImportSource(sourcePath); err != nil {
		return importPlan{}, err
	}
	realSource, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return importPlan{}, fmt.Errorf("resolve source %s: %w", sourcePath, err)
	}
	realVault, err := resolvedImportDestinationPath(vaultPath)
	if err != nil {
		return importPlan{}, err
	}
	if sameOrInside(vaultPath, sourcePath) || sameOrInside(realVault, realSource) {
		return importPlan{}, fmt.Errorf("refusing destination %s inside source %s; import must preserve the source fixture", vaultPath, sourcePath)
	}
	if sameOrInside(sourcePath, vaultPath) || sameOrInside(realSource, realVault) {
		return importPlan{}, fmt.Errorf("refusing overlapping source %s and destination %s", sourcePath, vaultPath)
	}
	if err := validateImportDestination(vaultPath); err != nil {
		return importPlan{}, err
	}

	plan := importPlan{sourcePath: sourcePath, vaultPath: vaultPath}
	for _, mapping := range importMappings {
		sourceDirPath := filepath.Join(sourcePath, mapping.source)
		info, err := os.Lstat(sourceDirPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return importPlan{}, fmt.Errorf("inspect source %s/: %w", mapping.source, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return importPlan{}, fmt.Errorf("refusing symlink source directory %s", sourceDirPath)
		}
		if !info.IsDir() {
			return importPlan{}, fmt.Errorf("source %s is not a directory", sourceDirPath)
		}
		count, err := countRegularFilesAndRefuseSymlinks(sourceDirPath)
		if err != nil {
			return importPlan{}, err
		}
		plan.items = append(plan.items, importPlanItem{
			sourceDir: mapping.source,
			destDir:   mapping.dest,
			fileCount: count,
		})
	}
	if len(plan.items) == 0 {
		return importPlan{}, fmt.Errorf("source %s has no importable ai/, notes/, profile/, or sessions/ directories", sourcePath)
	}
	return plan, nil
}

func validateImportSource(sourcePath string) error {
	info, err := os.Lstat(sourcePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("missing source %s", sourcePath)
	}
	if err != nil {
		return fmt.Errorf("inspect source %s: %w", sourcePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink source %s", sourcePath)
	}
	if !info.IsDir() {
		return fmt.Errorf("source %s is not a directory", sourcePath)
	}
	if err := refuseDirtyGitSource(sourcePath); err != nil {
		return err
	}
	return nil
}

func validateImportDestination(vaultPath string) error {
	parent := filepath.Dir(vaultPath)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("destination parent %s is not available: %w", parent, err)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("destination parent %s is not a directory", parent)
	}

	info, err := os.Lstat(vaultPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect destination %s: %w", vaultPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink destination %s", vaultPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing non-directory destination %s", vaultPath)
	}
	empty, err := isEmptyDir(vaultPath)
	if err != nil {
		return fmt.Errorf("inspect destination %s: %w", vaultPath, err)
	}
	if !empty {
		return fmt.Errorf("refusing non-empty destination %s to avoid duplicate or overwrite hazards", vaultPath)
	}
	return nil
}

func refuseDirtyGitSource(sourcePath string) error {
	if _, err := os.Lstat(filepath.Join(sourcePath, ".git")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect source git metadata: %w", err)
	}
	cmd := exec.Command("git", "-C", sourcePath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("inspect git source %s: %w", sourcePath, err)
	}
	if strings.TrimSpace(string(output)) != "" {
		return fmt.Errorf("refusing dirty git source %s; commit, stash, or clean changes before import", sourcePath)
	}
	return nil
}

func printImportPlan(w io.Writer, plan importPlan) {
	fmt.Fprintf(w, "Dry-run import plan (no filesystem changes)\n")
	fmt.Fprintf(w, "Source: %s\n", plan.sourcePath)
	fmt.Fprintf(w, "Destination: %s\n", plan.vaultPath)
	for _, item := range plan.items {
		fmt.Fprintf(w, "Plan: %s/ -> %s/ (%d files)\n", item.sourceDir, item.destDir, item.fileCount)
	}
	fmt.Fprintln(w, "Pass --apply to execute this migration after reviewing the plan.")
}

func applyImportPlan(plan importPlan) error {
	staging, err := os.MkdirTemp(filepath.Dir(plan.vaultPath), "."+filepath.Base(plan.vaultPath)+".dotvault-import-")
	if err != nil {
		return fmt.Errorf("create staging destination: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()

	for _, dir := range vaultDirectories {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o700); err != nil {
			return fmt.Errorf("stage %s/: %w", dir, err)
		}
	}
	for _, item := range plan.items {
		sourceDir := filepath.Join(plan.sourcePath, item.sourceDir)
		destDir := filepath.Join(staging, item.destDir)
		if err := copyTreeContents(sourceDir, destDir); err != nil {
			return fmt.Errorf("copy %s/ to %s/: %w", item.sourceDir, item.destDir, err)
		}
	}

	if _, err := os.Lstat(plan.vaultPath); err == nil {
		if err := os.Remove(plan.vaultPath); err != nil {
			return fmt.Errorf("remove empty destination before finalizing: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination before finalizing: %w", err)
	}
	if err := os.Rename(staging, plan.vaultPath); err != nil {
		return fmt.Errorf("finalize destination: %w", err)
	}
	cleanup = false
	return nil
}

func countRegularFilesAndRefuseSymlinks(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in source tree %s", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular source entry %s", path)
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("inspect source tree %s: %w", root, err)
	}
	return count, nil
}

func copyTreeContents(sourceDir, destDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %s", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular source entry %s", path)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(source, dest string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func validateExportInputs(vaultPath, outPath string) error {
	info, err := os.Lstat(vaultPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("missing vault %s", vaultPath)
	}
	if err != nil {
		return fmt.Errorf("inspect vault %s: %w", vaultPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink vault %s", vaultPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("vault %s is not a directory", vaultPath)
	}
	if sameOrInside(outPath, vaultPath) {
		return fmt.Errorf("refusing output %s inside vault %s", outPath, vaultPath)
	}
	if sameOrInside(vaultPath, outPath) {
		return fmt.Errorf("refusing output %s that contains vault %s", outPath, vaultPath)
	}
	realVault, err := filepath.EvalSymlinks(vaultPath)
	if err != nil {
		return fmt.Errorf("resolve vault %s: %w", vaultPath, err)
	}
	realOut, err := resolvedOutputPath(outPath)
	if err != nil {
		return err
	}
	if sameOrInside(realOut, realVault) {
		return fmt.Errorf("refusing output %s because it resolves inside vault %s", outPath, vaultPath)
	}
	if sameOrInside(realVault, realOut) {
		return fmt.Errorf("refusing output %s because it resolves above vault %s", outPath, vaultPath)
	}

	parent := filepath.Dir(outPath)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("output parent %s is not available: %w", parent, err)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("output parent %s is not a directory", parent)
	}

	outInfo, err := os.Lstat(outPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output %s: %w", outPath, err)
	}
	if outInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink output %s", outPath)
	}
	if !outInfo.IsDir() {
		return fmt.Errorf("refusing non-directory output %s", outPath)
	}
	empty, err := isEmptyDir(outPath)
	if err != nil {
		return fmt.Errorf("inspect output %s: %w", outPath, err)
	}
	if !empty {
		return fmt.Errorf("refusing non-empty output %s", outPath)
	}
	return nil
}

func writeTemplateExport(outPath string) error {
	staging, err := os.MkdirTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".dotvault-export-")
	if err != nil {
		return fmt.Errorf("create staging output: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()

	files := map[string]string{
		"README.md": "# dotvault private vault\n\nThis is a starter template for a private dotvault vault. Store real private content only in your private vault, not in the public tooling checkout.\n",
		"AGENTS.md": "# Vault Agent Contract\n\nAgents may read and write vault content under notes/, memory/, profile/, and sessions/ according to the user's local policy.\n",
	}
	for rel, content := range files {
		if err := writeTextFile(filepath.Join(staging, rel), content, 0o644); err != nil {
			return err
		}
	}
	for _, dir := range vaultDirectories {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o700); err != nil {
			return fmt.Errorf("create template %s/: %w", dir, err)
		}
		if err := writeTextFile(filepath.Join(staging, dir, ".gitkeep"), "", 0o644); err != nil {
			return err
		}
	}

	if _, err := os.Lstat(outPath); err == nil {
		if err := os.Remove(outPath); err != nil {
			return fmt.Errorf("remove empty output before finalizing: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output before finalizing: %w", err)
	}
	if err := os.Rename(staging, outPath); err != nil {
		return fmt.Errorf("finalize output: %w", err)
	}
	cleanup = false
	return nil
}

func writeTextFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
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

func resolveImportSourcePath(explicit string, getenv envLookup) (string, error) {
	sourcePath := strings.TrimSpace(explicit)
	if sourcePath == "" {
		if envPath, ok := getenv("KNOWLEDGE_DIR"); ok {
			sourcePath = strings.TrimSpace(envPath)
		}
	}
	if sourcePath == "" {
		if envPath, ok := getenv("KNOWLEDGE_REPO"); ok {
			sourcePath = strings.TrimSpace(envPath)
		}
	}
	if sourcePath == "" {
		return "", errors.New("missing source path; pass --from <path>")
	}
	return expandAndAbs(sourcePath, getenv)
}

func resolveRequiredPath(explicit, flagName string, getenv envLookup) (string, error) {
	path := strings.TrimSpace(explicit)
	if path == "" {
		return "", fmt.Errorf("missing path; pass %s <path>", flagName)
	}
	return expandAndAbs(path, getenv)
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

func sameOrInside(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func resolvedImportDestinationPath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve destination %s: %w", path, err)
	}
	parent := filepath.Dir(path)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("resolve destination parent %s: %w", parent, err)
	}
	return filepath.Clean(filepath.Join(realParent, filepath.Base(path))), nil
}

func resolvedOutputPath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve output %s: %w", path, err)
	}
	parent := filepath.Dir(path)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("resolve output parent %s: %w", parent, err)
	}
	return filepath.Clean(filepath.Join(realParent, filepath.Base(path))), nil
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
  DOTVAULT_REMOTE and DOTVAULT_BRANCH configure sync remote and branch.
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
  KNOWLEDGE_DIR or KNOWLEDGE_REPO may provide a legacy source path when --from is omitted.

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
