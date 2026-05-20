package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var syncAllowedPathspecs = []string{
	"notes",
	"memory",
	"profile",
	"sessions",
	".dotvault/config.json",
}

type syncOptions struct {
	vault  string
	remote string
	branch string
}

func runSync(args []string, getenv envLookup, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printSyncHelp(stdout)
		return 0
	}

	action := args[0]
	switch action {
	case "status", "pull", "push", "run":
	default:
		fmt.Fprintf(stderr, "dotvault sync: unknown subcommand %q\n\n", action)
		printSyncHelp(stderr)
		return 2
	}

	var vaultFlag string
	var remoteFlag string
	var branchFlag string
	fs := flag.NewFlagSet("sync "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&vaultFlag, "vault", "", "dotvault git repository path")
	fs.StringVar(&remoteFlag, "remote", "", "git remote name")
	fs.StringVar(&branchFlag, "branch", "", "git branch name")
	fs.Usage = func() {
		printSyncHelp(stdout)
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "dotvault sync %s: unexpected argument %q\n", action, fs.Arg(0))
		return 2
	}

	opts, err := resolveSyncOptions(vaultFlag, remoteFlag, branchFlag, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "dotvault sync %s: %v\n", action, err)
		return 2
	}
	if err := ensureGitWorktree(opts.vault); err != nil {
		fmt.Fprintf(stderr, "dotvault sync %s: %v\n", action, err)
		return 1
	}

	var runErr error
	switch action {
	case "status":
		runErr = syncStatus(opts, stdout)
	case "pull":
		runErr = syncPull(opts, stdout)
	case "push":
		runErr = syncPush(opts, stdout)
	case "run":
		runErr = syncRun(opts, stdout)
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "dotvault sync %s: %v\n", action, runErr)
		return 1
	}
	return 0
}

func resolveSyncOptions(vaultFlag, remoteFlag, branchFlag string, getenv envLookup) (syncOptions, error) {
	vaultPath := strings.TrimSpace(vaultFlag)
	if vaultPath == "" {
		if envPath, ok := getenv("DOTVAULT_PATH"); ok {
			vaultPath = strings.TrimSpace(envPath)
		}
	}
	if vaultPath == "" {
		if envPath, ok := getenv("KNOWLEDGE_DIR"); ok {
			vaultPath = strings.TrimSpace(envPath)
		}
	}
	if vaultPath == "" {
		if envPath, ok := getenv("KNOWLEDGE_REPO"); ok {
			vaultPath = strings.TrimSpace(envPath)
		}
	}
	if vaultPath == "" {
		return syncOptions{}, errors.New("missing vault path; pass --vault <path>, set DOTVAULT_PATH, or set legacy KNOWLEDGE_DIR/KNOWLEDGE_REPO")
	}
	vaultPath, err := expandAndAbs(vaultPath, getenv)
	if err != nil {
		return syncOptions{}, fmt.Errorf("resolve vault path: %w", err)
	}

	remote := strings.TrimSpace(remoteFlag)
	if remote == "" {
		if envRemote, ok := getenv("DOTVAULT_REMOTE"); ok {
			remote = strings.TrimSpace(envRemote)
		}
	}
	if remote == "" {
		if envRemote, ok := getenv("KNOWLEDGE_REMOTE"); ok {
			remote = strings.TrimSpace(envRemote)
		}
	}
	if remote == "" {
		remote = "origin"
	}

	branch := strings.TrimSpace(branchFlag)
	if branch == "" {
		if envBranch, ok := getenv("DOTVAULT_BRANCH"); ok {
			branch = strings.TrimSpace(envBranch)
		}
	}
	if branch == "" {
		if envBranch, ok := getenv("KNOWLEDGE_BRANCH"); ok {
			branch = strings.TrimSpace(envBranch)
		}
	}
	if branch == "" {
		branch = "main"
	}
	return syncOptions{vault: vaultPath, remote: remote, branch: branch}, nil
}

func syncStatus(opts syncOptions, stdout io.Writer) error {
	status, err := gitOutputString(opts.vault, "status", "--porcelain")
	if err != nil {
		return err
	}
	head, err := gitOutputString(opts.vault, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	shortHead, err := gitOutputString(opts.vault, "rev-parse", "--short", "HEAD")
	if err != nil {
		return err
	}
	remoteURL, err := gitOutputString(opts.vault, "remote", "get-url", opts.remote)
	if err != nil {
		remoteURL = "(not configured)\n"
	}

	fmt.Fprintf(stdout, "Vault: %s\n", opts.vault)
	fmt.Fprintf(stdout, "Remote: %s\n", opts.remote)
	fmt.Fprintf(stdout, "Remote URL: %s\n", strings.TrimSpace(remoteURL))
	fmt.Fprintf(stdout, "Branch: %s\n", opts.branch)
	fmt.Fprintf(stdout, "Current HEAD: %s (%s)\n", strings.TrimSpace(head), strings.TrimSpace(shortHead))
	if strings.TrimSpace(status) == "" {
		fmt.Fprintln(stdout, "Status: clean")
	} else {
		fmt.Fprintln(stdout, "Status: dirty")
		fmt.Fprint(stdout, status)
	}
	return nil
}

func syncPull(opts syncOptions, stdout io.Writer) error {
	status, err := gitOutputString(opts.vault, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("refusing pull with dirty local state; commit, stash, or clean changes before pulling\n%s", status)
	}
	remoteRef, err := fetchRemoteBranch(opts)
	if err != nil {
		return err
	}
	preHead, _ := gitOutputString(opts.vault, "rev-parse", "HEAD")
	remoteHead, _ := gitOutputString(opts.vault, "rev-parse", remoteRef)
	if err := gitRun(opts.vault, "rebase", remoteRef); err != nil {
		return handleRebaseFailure(opts, strings.TrimSpace(preHead), strings.TrimSpace(remoteHead), err)
	}
	fmt.Fprintf(stdout, "Pulled %s/%s with rebase.\n", opts.remote, opts.branch)
	return nil
}

func syncPush(opts syncOptions, stdout io.Writer) error {
	if err := verifyPushSafe(opts); err != nil {
		return err
	}
	if err := gitRun(opts.vault, "push", opts.remote, "HEAD:refs/heads/"+opts.branch); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Pushed HEAD to %s/%s.\n", opts.remote, opts.branch)
	return nil
}

func syncRun(opts syncOptions, stdout io.Writer) error {
	gitDir, err := gitDirPath(opts.vault)
	if err != nil {
		return err
	}
	lockPath := filepath.Join(gitDir, "dotvault-sync.lock")
	return withSyncLock(lockPath, func() error {
		fmt.Fprintf(stdout, "Acquired sync lock: %s\n", displayLockPath(opts.vault, lockPath))
		committed, err := commitAllowedVaultChanges(opts)
		if err != nil {
			return err
		}
		if committed {
			fmt.Fprintln(stdout, "Committed allowed vault changes.")
		} else {
			fmt.Fprintln(stdout, "No allowed vault changes to commit.")
		}

		remoteRef, err := fetchRemoteBranch(opts)
		if err != nil {
			return err
		}
		preHead, _ := gitOutputString(opts.vault, "rev-parse", "HEAD")
		remoteHead, _ := gitOutputString(opts.vault, "rev-parse", remoteRef)
		if err := gitRun(opts.vault, "rebase", remoteRef); err != nil {
			return handleRebaseFailure(opts, strings.TrimSpace(preHead), strings.TrimSpace(remoteHead), err)
		}
		if err := verifyPushSafe(opts); err != nil {
			return err
		}
		if err := gitRun(opts.vault, "push", opts.remote, "HEAD:refs/heads/"+opts.branch); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Pushed HEAD to %s/%s.\n", opts.remote, opts.branch)
		return nil
	})
}

func ensureGitWorktree(vaultPath string) error {
	info, err := os.Stat(vaultPath)
	if err != nil {
		return fmt.Errorf("vault %s is not available: %w", vaultPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("vault %s is not a directory", vaultPath)
	}
	if err := gitRun(vaultPath, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("vault %s is not a git worktree: %w", vaultPath, err)
	}
	return nil
}

func gitDirPath(vaultPath string) (string, error) {
	out, err := gitOutputString(vaultPath, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(out)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(vaultPath, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func withSyncLock(lockPath string, fn func() error) error {
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return fmt.Errorf("sync already in progress; remove %s only after verifying no dotvault sync is running", displayLockPath(filepath.Dir(filepath.Dir(lockPath)), lockPath))
	}
	if err != nil {
		return fmt.Errorf("create sync lock %s: %w", lockPath, err)
	}
	_, writeErr := fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(lockPath)
		return fmt.Errorf("write sync lock %s: %w", lockPath, writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(lockPath)
		return fmt.Errorf("close sync lock %s: %w", lockPath, closeErr)
	}
	defer os.Remove(lockPath)
	return fn()
}

func displayLockPath(vaultPath, lockPath string) string {
	rel, err := filepath.Rel(vaultPath, lockPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return lockPath
	}
	return filepath.ToSlash(rel)
}

func commitAllowedVaultChanges(opts syncOptions) (bool, error) {
	if disallowed, err := stagedDisallowedPaths(opts.vault); err != nil {
		return false, err
	} else if len(disallowed) > 0 {
		return false, fmt.Errorf("refusing sync run with staged disallowed paths outside notes/, memory/, profile/, sessions/, and .dotvault/config.json: %s", strings.Join(disallowed, ", "))
	}

	paths, err := changedAllowedPaths(opts.vault)
	if err != nil {
		return false, err
	}
	if len(paths) > 0 {
		args := []string{"add", "--all", "--"}
		args = append(args, paths...)
		if err := gitRun(opts.vault, args...); err != nil {
			return false, err
		}
	}
	if disallowed, err := stagedDisallowedPaths(opts.vault); err != nil {
		return false, err
	} else if len(disallowed) > 0 {
		return false, fmt.Errorf("refusing sync run with staged disallowed paths outside notes/, memory/, profile/, sessions/, and .dotvault/config.json: %s", strings.Join(disallowed, ", "))
	}

	if err := gitRun(opts.vault, "diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	if err := gitRun(opts.vault, "commit", "-m", "Sync dotvault changes"); err != nil {
		return false, err
	}
	return true, nil
}

func stagedDisallowedPaths(vaultPath string) ([]string, error) {
	output, err := gitOutputString(vaultPath, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	var disallowed []string
	for _, line := range strings.Split(output, "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if !allowedSyncPath(path) {
			disallowed = append(disallowed, path)
		}
	}
	return disallowed, nil
}

func changedAllowedPaths(vaultPath string) ([]string, error) {
	status, err := gitOutputString(vaultPath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var paths []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if before, after, ok := strings.Cut(path, " -> "); ok {
			if allowedSyncPath(before) && !seen[before] {
				paths = append(paths, before)
				seen[before] = true
			}
			path = after
		}
		if allowedSyncPath(path) && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}
	return paths, nil
}

func allowedSyncPath(path string) bool {
	path = filepath.ToSlash(strings.Trim(path, `"`))
	for _, allowed := range syncAllowedPathspecs {
		allowed = filepath.ToSlash(allowed)
		if path == allowed || strings.HasPrefix(path, allowed+"/") {
			return true
		}
	}
	return false
}

func fetchRemoteBranch(opts syncOptions) (string, error) {
	refSpec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", opts.branch, opts.remote, opts.branch)
	if err := gitRun(opts.vault, "fetch", opts.remote, refSpec); err != nil {
		return "", fmt.Errorf("missing upstream %s/%s or fetch failed; configure a local/known upstream before syncing: %w", opts.remote, opts.branch, err)
	}
	return fmt.Sprintf("refs/remotes/%s/%s", opts.remote, opts.branch), nil
}

func verifyPushSafe(opts syncOptions) error {
	remoteRef, err := fetchRemoteBranch(opts)
	if err != nil {
		return err
	}
	if err := gitRun(opts.vault, "merge-base", "--is-ancestor", remoteRef, "HEAD"); err != nil {
		return fmt.Errorf("refusing non-fast-forward push to %s/%s; run dotvault sync pull first and resolve conflicts explicitly: %w", opts.remote, opts.branch, err)
	}
	return nil
}

func handleRebaseFailure(opts syncOptions, preHead, remoteHead string, rebaseErr error) error {
	_ = gitRun(opts.vault, "rebase", "--abort")
	if preHead == "" {
		preHead = "HEAD"
	}
	if remoteHead == "" {
		remoteHead = "remote"
	}
	branch := fmt.Sprintf("dotvault-conflict-%s-%s", shortRef(preHead), shortRef(remoteHead))
	if err := gitRun(opts.vault, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		if createErr := gitRun(opts.vault, "branch", branch, preHead); createErr != nil {
			return fmt.Errorf("rebase conflict and failed to create recovery branch: %w\noriginal error: %v", createErr, rebaseErr)
		}
	}
	return fmt.Errorf(`conflict while rebasing onto %s/%s; local state is preserved on branch %s
Recovery commands:
  git -C %q checkout %s
  git -C %q fetch %s %s
  git -C %q rebase %s/%s
  # resolve files, then: git -C %q rebase --continue
  dotvault sync push --vault %q --remote %s --branch %s
Original git error: %w`, opts.remote, opts.branch, branch,
		opts.vault, branch,
		opts.vault, opts.remote, opts.branch,
		opts.vault, opts.remote, opts.branch,
		opts.vault,
		opts.vault, opts.remote, opts.branch,
		rebaseErr)
}

func shortRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if len(ref) > 12 {
		return ref[:12]
	}
	if ref == "" {
		return "unknown"
	}
	return ref
}

func gitRun(dir string, args ...string) error {
	cmd := gitCommand(dir, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutputString(dir string, args ...string) (string, error) {
	cmd := gitCommand(dir, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func gitCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}
