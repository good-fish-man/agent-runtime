# Intent and Capability Routing

Athena routes every user turn before exposing capabilities to the model.

```text
User
  |
  v
Intent Parser
  |
  v
Capability Router
  |
  +----------------+----------------+
  |                |                |
  v                v                v
Research        Browser            File
  |                |                |
  v                v                v
Search System   Browser System   Filesystem/Desktop
```

## Intent Parser

`internal/intent` converts the current request into a provider-independent `Intent`:

- `Goal` and normalized text
- candidate `Domains`
- execution `Mode`
- semantic `Signals`
- extracted `Entities`
- parser `Confidence`

The parser may use active browser/desktop sessions and previous user messages, but the current explicit intent always wins. For example, a short `Tokyo` reply can refine a research task, while `Open YouTube home page` switches to browser execution.

## Capability Router

`internal/router` converts `Intent` into a deterministic `RoutePlan`:

```json
{
  "primary": "browser",
  "capabilities": ["interaction.ask", "browser.task", "browser.open"],
  "fallbacks": [],
  "excluded_capabilities": ["internet.search", "internet.fetch", "browser.search", "desktop.action"],
  "reason": "direct_browser_interaction"
}
```

`policy.go` owns route precedence and conflict resolution. `router.go` owns capability composition. The Dispatcher consumes the plan and must not reclassify the request.

## Route Precedence

1. Direct or authenticated browser interaction
2. Scheduled operations
3. Local device and workspace file operations
4. Explicit desktop applications
5. Research and current external knowledge
6. Generic open targets
7. Planning and task management
8. Conversation fallback

Configured Agent capabilities are appended after routing. Capabilities listed in `ExcludedCapabilities` are removed again so configuration cannot override safety or route isolation.
