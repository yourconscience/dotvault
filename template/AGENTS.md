# Private Vault Agent Contract

This file is a generic template for a private vault. It belongs in the private vault copy, not as a place to write real private content inside the public tooling checkout.

## Directory contract

- Write human-authored or user-approved notes under `notes/`.
- Write durable agent memory, summaries, and digests under `memory/`.
- Write durable profile facts only when the user explicitly wants them kept under `profile/`.
- Write session summaries and handoffs under `sessions/`.

## Safety

Agents should keep raw transcripts, credentials, tokens, and unrelated project data out of the vault unless an explicit private-vault policy allows them. Public exports should use templates or synthetic examples instead of private content.
