# MCP verification for the Go port

Verified 2026-11-10 before legacy removal. This checks the six configured local MCP servers; unrelated hosted apps are not language servers and were not exercised or reconfigured.

| Server | Result | Go setup |
| --- | --- | --- |
| context-mode | Doctor passed all 12 rows, including hook round trips and database/index | Language-neutral; no Go-specific changes required |
| Context7 | Go and pgx documentation resolution and queries succeeded | Use current Go/provider/library documentation during implementation |
| Serena | Go probe and permanent audit-command symbol lookups succeeded; standalone Go-only project health check passed | Existing gopls updated from v0.21.1 to latest v0.24.0; local project language file set to Go; live server still retains its prior TypeScript registration until reconnect, while Go symbol analysis already works |
| jcodemunch | Index refreshed and Go probe symbol found | Go parsing works; refresh after cleanup and subsequent implementation changes |
| CodeGraphContext | Index job completed with no errors and Go probe query succeeded | Go parser probe passed; cleanup synchronized and live watching enabled; private/runtime paths excluded with .cgcignore |
| clojure-mcp | nREPL discovery responded; no running REPLs found | Clojure-specific and unnecessary for Go; no REPL started and no global server configuration removed |

Serena project settings are local/ignored under `.serena/`; gopls is installed in the existing user Go tools directory. New checkouts need Go and gopls on PATH, a Go Serena project configuration and refreshed source indexes. gopls check also completed successfully on the temporary probe, which was removed after verification.

CodeGraphContext CLI doctor reported an exclusive local database lock while its live MCP server owned that database. This did not indicate an MCP outage: indexing and subsequent source queries through that owner succeeded. Do not start a second standalone database owner to work around the lock. Use the MCP server for indexing and queries.

jcodemunch's launchd watch daemon was inactive. Manual index refresh works; this is not a failed server or a promise of automatic freshness. Refresh explicitly when needed.

No private credentials were printed or added to MCP configuration. Project tool caches remain ignored. No global Clojure settings or unrelated hosted app permissions were changed.

After cleanup, the permanent Go repository-audit command and its symbols were found through Serena and CodeGraphContext; jcodemunch was refreshed again. The temporary probe is absent from the synchronized graph. Standalone Serena health-check started only the Go language server and passed all its tool checks.
