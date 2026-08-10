# Athena Action/Observation v3

Version 3 preserves the v2 Decision, Action, and Perception ownership boundaries while adding bounded visual evidence to terminal Observations.

## Migration

- Launcher, Runtime Client, and Agent Runtime must be upgraded together.
- Every envelope uses `protocol: athena.agent.v3`; v2 envelopes are rejected.
- `Observation.attachments` may contain at most two PNG/JPEG/WebP images of at most 4 MiB each.
- Runtime Client validates Base64, decoded size, MIME type, and SHA-256 before use.
- Attachment bytes are transient. Persistence, task history, logs, and frontend events retain metadata only.
- Visual evidence is forwarded only when the active default model declares `vision`, `multimodal`, or `image-input`.
- Agent Runtime maps evidence to Eino native `UserInputMultiContent`; it is never embedded in prompt text.

The browser Perception schema is independently versioned as `athena.perception.v5`, covering adaptive budgets, page stabilization, verification, recovery, and repetition circuit breaking.
