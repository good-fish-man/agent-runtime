# Memory, Context, and Retrieval

## Purpose

This subsystem gives a run useful historical and external context without allowing the prompt to grow forever. It separates durable user memory, transient conversation compression, knowledge retrieval, and uploaded-file context.

## Main Locations

| Location | Responsibility |
| --- | --- |
| [`internal/memory/model.go`](../../internal/memory/model.go) | GORM entities for memory and indexes |
| [`internal/memory/store.go`](../../internal/memory/store.go) | Scoped durable memory operations and snapshots |
| [`internal/memory/extractor.go`](../../internal/memory/extractor.go) | Structured memory extraction through the request model |
| [`internal/memory/reviewer.go`](../../internal/memory/reviewer.go) | Asynchronous post-response review |
| [`internal/memory/snapshot.go`](../../internal/memory/snapshot.go) | Stable prompt snapshot rendering |
| [`internal/contextcompressor`](../../internal/contextcompressor) | Token pressure, conversion, and compaction orchestration |
| [`internal/contextcompressor/compactors`](../../internal/contextcompressor/compactors) | Micro, partial, and full compaction algorithms |
| [`internal/contextcompressor/prompt`](../../internal/contextcompressor/prompt) | Summary prompts and formatting |
| [`internal/retriever/retriever.go`](../../internal/retriever/retriever.go) | Knowledge-base retrieval |
| [`internal/retriever/retriever_file.go`](../../internal/retriever/retriever_file.go) | Uploaded and project-file context retrieval |

## Four Different Context Types

| Type | Lifetime | Source of truth | Example |
| --- | --- | --- | --- |
| Conversation | One session | Request messages | Recent user and assistant turns |
| Durable memory | Across sessions | PostgreSQL memory store | Stable preference or profile fact |
| Retrieved knowledge | One run or cache lifetime | KB/search provider | Documentation excerpt |
| File context | One run or workspace lifetime | Authorized file | Uploaded PDF or project source |

Do not store every retrieved page as user memory. Do not treat a compacted conversation summary as an immutable user fact.

## Durable Memory Model

The database stores memory records and an index used for scoped retrieval. Memory is partitioned by user, agent, and session context so one user or agent cannot consume another scope's history.

The store supports operations such as:

- add or replace a memory;
- remove a memory;
- search relevant memories;
- deduplicate semantically equivalent entries;
- scan content before persistence;
- build a frozen snapshot for a run.

A run consumes a snapshot rather than a mutable live view. This prevents background extraction from changing the prompt halfway through execution.

## Memory Extraction

After a successful user/assistant exchange, `BackgroundReviewer` may call `Extractor` to identify durable facts worth storing.

The extractor uses the exact request model configuration passed by the server. It does not search arbitrary local environment variables for a model or API key. This preserves user model isolation and makes usage attributable.

Extraction asks for structured output and should keep only information that is:

- useful across future sessions;
- attributable to the user interaction;
- safe to retain;
- not merely temporary task state;
- not an API key, password, session cookie, or other secret.

Model compatibility matters. The extractor does not force non-default parameters such as `temperature=0.3` on models that only accept their default value.

Extraction failure is logged with the request trace but does not invalidate a response already delivered to the user.

## Context Compression

The compressor estimates token pressure and selects a strategy:

| Strategy | Use |
| --- | --- |
| No compaction | Context is safely below the threshold |
| Micro | Remove low-value repetition and normalize messages |
| Partial | Summarize an older middle section while preserving recent turns |
| Full | Produce a bounded state summary when pressure is severe |

Compaction preserves:

- the latest user goal;
- system and governance instructions;
- unresolved approvals or Actions;
- tool-call and tool-result relationships;
- stable identifiers and file paths needed by the active task;
- recent conversation turns;
- explicit constraints and decisions.

It should discard redundant prose before discarding causal execution state.

The ADK converter translates between Eino messages and the compressor's neutral structures. `IntegrationService` chooses and applies compaction without making the dispatcher understand each algorithm.

## Knowledge Retrieval

The retriever calls configured knowledge-base endpoints with the current query and bounds the returned excerpts. It returns evidence context, not system instructions.

Knowledge retrieval should include:

- source or document identity;
- relevant excerpt;
- score when available;
- truncation information;
- a trust marker.

The dispatcher decides whether retrieved knowledge is relevant to the current route. It should not query every configured knowledge base on every chat turn.

## File Retrieval

File retrieval resolves uploaded-file descriptors and authorized project files. It is distinct from interactive filesystem tools:

| File retrieval | Filesystem tool |
| --- | --- |
| Adds bounded text context before the model call | Executes an explicit read/search/edit action during the loop |
| Usually read-only | May be read or write based on capability |
| Uses request attachments and known files | Uses model-selected paths under authorized roots |

Large or binary files should be converted or summarized through an appropriate skill rather than inserted raw into the prompt.

## Prompt Integration

The dispatcher inserts memory, retrieved knowledge, and file excerpts into separate sections. Each section has a size budget and trust class.

```text
system policy
  > reviewed runtime plan
  > user goal
  > execution observations
  > durable memory and retrieved evidence
  > arbitrary external text
```

This ordering describes authority, not necessarily literal prompt position.

## Invariants

1. Memory access is scoped by user and agent identity.
2. A run uses a frozen memory snapshot.
3. Extraction uses the resolved request model and never leaks another user's credentials.
4. Secrets and temporary tool state are not durable memories.
5. Compression preserves causal tool/action relationships.
6. Retrieved and file content remains untrusted evidence.
7. Every context source has a size budget.

## Safe Extension Points

Add a memory type by defining its retention rule, scope, deduplication key, prompt representation, and deletion behavior together.

Add a compaction strategy behind the existing compressor interface and test that it preserves system messages, recent turns, tool pairs, and pending Actions.

Add a retriever by returning normalized, bounded evidence records. Keep endpoint-specific payloads outside the prompt builder.

## Tests

Memory extractor tests use fake model responses. Context compressor tests validate pressure selection and preserved messages. Retriever changes should include bounds, cancellation, malformed payload, and unauthorized path cases.
