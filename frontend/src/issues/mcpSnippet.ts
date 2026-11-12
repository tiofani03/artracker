// Copy-ready setup snippets for connecting an MCP client (Claude Code, Cursor) to a project.

const ABSOLUTE = /^(https?:)\/\//i

/** Full URL of the MCP endpoint, resolving a relative API base against the page origin. */
export function mcpApiUrl(apiBase: string, origin: string): string {
  const m = ABSOLUTE.exec(apiBase)
  const [scheme, rest] = m ? [m[0], apiBase.slice(m[0].length)] : [origin.replace(/\/+$/, ''), apiBase]
  const path = `/${rest}/mcp`.replace(/\/{2,}/g, '/')
  if (m) return scheme + path.slice(1)
  return scheme + path
}

export function mcpServerName(slug: string): string {
  return `artracker-${slug}`
}

export function claudeCodeCommand(url: string, name: string, key: string): string {
  return `claude mcp add --transport http ${name} ${url} --header "Authorization: Bearer ${key}"`
}

export function cursorConfig(url: string, name: string, key: string): string {
  return JSON.stringify({ mcpServers: { [name]: { url, headers: { Authorization: `Bearer ${key}` } } } }, null, 2)
}
