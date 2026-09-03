package cmd

// mcpServerInstructions is handed to every MCP client at initialize. It is the
// only guidance a client that never loads the corgi skills — the Claude app
// connector on a phone, a bare MCP inspector — gets before its first call, so
// it carries the few facts that a tool description alone cannot: which call to
// make first, what is and is not a ready gate, and what the tunnel gate hides.
const mcpServerInstructions = `corgi runs a multi-service development stack from one corgi-compose.yml: databases in Docker, service repositories, generated env files with cross-service URLs.

Orient first. corgi_context answers "where am I" in one call (topology, ports, health, each repository's branch). When the user names a stack by a human name, corgi_workspace_resolve maps it to one registered workspace and returns candidates instead of guessing; echo the resolved path before working in it.

corgi_up is always detached and is not a ready gate: it runs every beforeStart (installs, migrations, builds) before returning, which takes minutes on a cold stack, then poll corgi_status until healthy. corgi_status is the only liveness truth; corgi_ps status only says whether a process or container exists. For one service that is down, corgi_why returns a single verdict. To wait for a log line, corgi_wait_for_log blocks instead of polling corgi_logs.

corgi_exec, corgi_db_query, corgi_pr_open, corgi_worktrees_* and corgi_preview_* are refused while the server is reachable over a public tunnel unless CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1 is set on the machine. A workspace marked sensitive refuses remote session start and previews by design.

corgi_diff shows a change without a tunnel or a running stack and is the default way to show work on a bad connection. Stop a preview when the user is done with it; a forgotten preview is a public URL onto seeded data.`
