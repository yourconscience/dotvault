---
name: dotvault
description: Use when working with dotvault private-vault conventions, public/private topology, hook payloads, search contracts, or migration-safe vault organization.
---

# dotvault

Use this skill when a task involves the `dotvault` vault format, public-safe templates, hook contracts, search contracts, or migration planning between a legacy knowledge-style layout and the dotvault nouns.

## Core conventions

- Public tooling checkout: `~/Workspace/dotvault`
- Private vault location: `~/Workspace/vault`
- Optional stable symlink: `~/.vault -> ~/Workspace/vault`
- Vault nouns: `notes/`, `memory/`, `profile/`, and `sessions/`
- Legacy migration naming: map `ai/` to `memory/`

## Privacy rules

Never place private notes, memories, profile facts, sessions, transcripts, credentials, or real remote addresses into the public tooling checkout. Use generic examples or temporary fixtures for demonstrations and tests.

## Hook rules

Hook entrypoints receive JSON on stdin for `session-start`, `stop`, and `session-end`. Detect Factory Droid, Amp, Hermes, and Claude Code by payload shape before writing any derived private-vault output.

## Search rules

Treat search as provider-agnostic with `index`, `search`, and `expand` operations. `memsearch` is the default provider name, not a requirement to expose private local state.
