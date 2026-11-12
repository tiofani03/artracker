import { describe, expect, it } from 'vitest'
import { claudeCodeCommand, cursorConfig, mcpApiUrl, mcpServerName } from './mcpSnippet'

describe('mcpApiUrl', () => {
  it('prefixes a relative base with the origin', () => {
    expect(mcpApiUrl('/api/v1', 'https://app.example.com')).toBe('https://app.example.com/api/v1/mcp')
  })
  it('keeps an absolute base and strips the trailing slash', () => {
    expect(mcpApiUrl('https://api.example.com/api/v1/', 'https://app.example.com')).toBe('https://api.example.com/api/v1/mcp')
  })
  it('strips duplicate slashes in the path but not the scheme', () => {
    expect(mcpApiUrl('//api//v1', 'https://app.example.com/')).toBe('https://app.example.com/api/v1/mcp')
    expect(mcpApiUrl('https://api.example.com//api//v1', 'x')).toBe('https://api.example.com/api/v1/mcp')
  })
})

describe('mcpServerName', () => {
  it('prefixes the slug', () => {
    expect(mcpServerName('shop')).toBe('artracker-shop')
  })
})

describe('claudeCodeCommand', () => {
  it('builds the claude mcp add command', () => {
    expect(claudeCodeCommand('https://a.io/api/v1/mcp', 'artracker-shop', 'art_mcp_x')).toBe(
      'claude mcp add --transport http artracker-shop https://a.io/api/v1/mcp --header "Authorization: Bearer art_mcp_x"',
    )
  })
})

describe('cursorConfig', () => {
  it('builds pretty JSON', () => {
    const out = cursorConfig('https://a.io/api/v1/mcp', 'artracker-shop', 'art_mcp_x')
    expect(out).toContain('\n  "mcpServers"')
    expect(JSON.parse(out)).toEqual({
      mcpServers: { 'artracker-shop': { url: 'https://a.io/api/v1/mcp', headers: { Authorization: 'Bearer art_mcp_x' } } },
    })
  })
})
