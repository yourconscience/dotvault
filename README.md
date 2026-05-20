# dotvault

> **Early / Experimental** -- This project is under active development. APIs, CLI commands, and vault format may change without notice.

`dotvault` is a public, privacy-first toolkit for defining a personal vault format, hook contracts, search contracts, and future CLI workflows.

This repository is the public tooling checkout. It is not a private vault and must not contain private notes, memories, profiles, sessions, transcripts, credentials, or machine-local operational details.

## Public/private topology

The intended topology is:

- Public tooling checkout: `~/Workspace/dotvault`
- Private vault: `~/Workspace/vault`
- Stable local symlink: `~/.vault -> ~/Workspace/vault`

Keep private content in the private vault, outside this public checkout. The template in `template/` is starter material for creating a private vault elsewhere; do not fill it with real personal content inside this repository.

## Repository surface

- `spec/VAULT.md` defines the vault nouns and migration naming.
- `spec/HOOKS.md` defines lifecycle hook payload and dispatch contracts.
- `spec/SEARCH.md` defines provider-agnostic search operations.
- `template/` contains generic starter vault directories.
- `hooks/` contains portable hook entrypoint and adapter stubs.
- `skill/` contains a public agent skill surface and trigger-only eval prompts.
- `cmd/dotvault/` implements the Go CLI and fixture-based tests.

## Install

```sh
# From source
make build          # produces ./dotvault
make install        # copies to $GOPATH/bin or ~/go/bin

# Or directly
go install github.com/yourconscience/dotvault/cmd/dotvault@latest
```

## CLI behavior

The MVP CLI is implemented under `cmd/dotvault`:

- `dotvault init --vault <path> [--link-home]` creates `notes/`, `memory/`, `profile/`, `sessions/`, and `.dotvault/config.json`. `--link-home` is opt-in and refuses unsafe existing `~/.vault` targets.
- `dotvault import --from <knowledge-path> --vault <vault-path> [--apply]` is dry-run/status-first by default, preserves the source, maps legacy `ai/` to `memory/`, and refuses unsafe or overlapping source/destination states.
- `dotvault export --vault <path> --out <path>` writes public/template-safe starter content only. It does not copy private `notes/`, `memory/`, `profile/`, or `sessions/` content and refuses non-empty or symlink-escape output targets.
- `dotvault sync status|pull|push|run` wraps git sync for a private vault, preferring `DOTVAULT_PATH`, `DOTVAULT_REMOTE`, and `DOTVAULT_BRANCH` with `KNOWLEDGE_` fallbacks for migration compatibility. `sync run` uses `.git/dotvault-sync.lock` and stages only allowlisted vault paths instead of using blind `git add -A`.

Validation uses local temporary fixtures, isolated `HOME` values, controlled environment variables, and local bare git remotes to model sync. It requires no network services, external ports, GitHub remotes, VPS access, browsers, databases, or API credentials.

## Safety rules

- Do not copy private vault content into this repository.
- Do not configure a remote unless an explicit publishing step requests it.
- Use temporary fixtures for tests that model migration or sync; sync tests use local bare remotes only.
- Keep search indexes and generated artifacts out of the public source tree unless they are generic fixtures.
