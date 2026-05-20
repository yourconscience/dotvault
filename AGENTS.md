# Agent Instructions

This is the public `dotvault` tooling repository. Work only on public-safe specs, templates, hook stubs, skill files, and tooling code in this checkout.

## Boundaries

- The public checkout is `~/Workspace/dotvault`.
- A user's private vault belongs outside this checkout, for example `~/Workspace/vault`.
- `~/.vault` may be used as a stable symlink to a private vault.
- Legacy migration examples may refer generically to `~/Workspace/knowledge`.

Never write real private notes, memory, profile facts, sessions, transcripts, credentials, machine identifiers, or remote addresses into this repository. Do not instruct users or agents to store private notes in the public checkout.

## Template contract

`template/` is a generic starter for a private vault. It may contain placeholder files and instructions only. If you need realistic examples, create synthetic examples that cannot be mistaken for copied personal data.

## Hook and CLI work

Hook entrypoints must accept JSON on stdin and avoid hard-coded private paths. CLI and sync tests must use temporary fixtures, isolated `HOME`, controlled environment variables, and local bare git remotes only.

## Validation

Before handing off changes, run the relevant validators for shell syntax, Python syntax, JSON validity, Go tests when Go code is present, and static privacy scans for prohibited private identifiers.
