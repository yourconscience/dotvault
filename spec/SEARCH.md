# Search Contract

`dotvault` search is provider-agnostic. A private vault can be indexed by any implementation that satisfies the operations below.

## Operations

### `index`

Build or refresh a derived search index from selected vault content. Indexes are generated artifacts and should not be treated as canonical private content.

### `search`

Return ranked matches for a query against an index. Results should include enough metadata for a caller to identify the source vault file and match context without exposing unrelated private content.

### `expand`

Expand a selected result into a larger context window or neighboring records so an agent can use it safely.

## Default provider

The default provider name is `memsearch`. It is a provider choice, not a requirement to use any private local state. Alternative providers may implement the same `index`, `search`, and `expand` operations.

## Privacy expectations

Search implementations should index private vaults outside the public checkout, avoid committing indexes to this repository, and keep provider configuration free of machine-specific private identifiers.
