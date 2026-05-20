# dotvault

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
- `cmd/dotvault/` is minimal Go scaffolding for later CLI work.

## Current status

This skeleton establishes public contracts and validation surfaces. CLI behavior beyond a placeholder executable is planned for later implementation and should not be treated as available until the command code and tests are added.

## Safety rules

- Do not copy private vault content into this repository.
- Do not configure a remote unless an explicit publishing step requests it.
- Use temporary fixtures for tests that model migration or sync.
- Keep search indexes and generated artifacts out of the public source tree unless they are generic fixtures.
