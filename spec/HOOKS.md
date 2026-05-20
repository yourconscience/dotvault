# Hook Contract

`dotvault` hooks are public entrypoint contracts for agents that want to write summaries or memory into a private vault. The stubs in this repository are safe examples and do not write private content by default.

## Lifecycle events

The v1 lifecycle events are:

- `session-start` — an agent session has started.
- `stop` — an agent stop event occurred and may include partial context.
- `session-end` — an agent session has ended and may include final context.

Each event has a matching shell entrypoint in `hooks/`:

- `hooks/session-start.sh`
- `hooks/stop.sh`
- `hooks/session-end.sh`

## Input contract

Hooks receive a single JSON payload on stdin. Entrypoints should read stdin exactly once, validate that a payload was received, detect the source agent from payload shape, and delegate to an adapter. Hook code must avoid hard-coded private vault paths; configuration should come from environment variables or future CLI config.

## Agent payload detection

Detection is intentionally shape-based and provider-agnostic:

- Factory Droid: `transcript_path` contains `.factory/` and ends with `.jsonl`.
- Amp: `platform == "amp"` or an `amp_thread_id` field is present.
- Hermes: `session_id` is present and `transcript_path` is absent.
- Claude Code: fallback behavior for JSON payloads that do not match Factory Droid, Amp, or Hermes.

Adapters may evolve as agent payloads change, but they should preserve this public ordering so specific providers are selected before the Claude Code fallback.

## Output contract

Stubs should return success for recognized no-op handling and print concise diagnostic messages to stderr. Future implementations may write derived summaries into a selected private vault, but must not copy raw transcripts into the public checkout.

## Configuration

Portable hook code should prefer:

- `DOTVAULT_PATH` for the private vault path.
- `DOTVAULT_HOOK_EVENT` for the lifecycle event passed from an entrypoint to an adapter.
- Temporary files only when needed, with cleanup traps.

No hook should assume that `~/Workspace/dotvault` is a private vault.
