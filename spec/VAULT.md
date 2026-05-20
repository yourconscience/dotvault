# Vault Format

This document defines the public `dotvault` private-vault layout. The public repository contains tooling and templates only; a real vault should be created outside the public checkout.

## Topology

- Public tooling checkout: `~/Workspace/dotvault`
- Private vault: `~/Workspace/vault`
- Stable local symlink: `~/.vault -> ~/Workspace/vault`

The public checkout must not be used as the storage location for private notes.

## Required nouns

A dotvault vault uses four top-level noun directories:

- `notes/` — human-authored notes, drafts, and references.
- `memory/` — agent-written durable memory, digests, and derived summaries.
- `profile/` — durable identity, preference, and context facts the owner chooses to store.
- `sessions/` — session summaries and handoff records.

The nouns are intentionally generic so providers, agents, and local tooling can agree on a stable structure without depending on one vendor.

## Migration naming

The public dotvault format uses `memory/` as the replacement name for legacy knowledge-vault `ai/` content. Migration tooling should map legacy `ai/` files into `memory/` in a temporary or explicitly selected private vault destination, while preserving the source fixture or source vault by default.

## Public template

The `template/` directory in this repository mirrors the required nouns with placeholder files only. It is a starter for a private vault created elsewhere; it is not a place to store real private content.

## Safety invariants

- Private vault content stays outside the public tooling checkout.
- Import and migration workflows should be dry-run or status-first by default.
- Export workflows should include public/template material only unless a future explicit private export mode is designed and tested.
- Tests should use temporary fixtures instead of live vault paths.
