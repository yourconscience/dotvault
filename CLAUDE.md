# Claude Guidance

This repository is public tooling for `dotvault`, not a private vault.

## Work safely

- Keep private content out of this checkout.
- Use `~/Workspace/dotvault` only for public specs, templates, hooks, skills, and tooling.
- A private vault should live elsewhere, such as `~/Workspace/vault`, with `~/.vault` as an optional stable symlink.
- Do not tell agents to write real notes, memory, profile data, or session records into this public repository.

## Expected edits

Acceptable changes include public documentation, format specs, generic templates, hook stubs, skill metadata, and implementation code that is tested against temporary fixtures. Avoid machine-local paths, real transcript paths, private remotes, credentials, and copied personal data.

## Validation

Prefer local validators and temporary fixtures. Do not contact private remotes or mutate live vault paths while developing this public project.
