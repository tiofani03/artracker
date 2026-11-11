function positiveInt(value: string | null): number | null {
  if (value === null || !/^\d+$/.test(value)) return null
  const n = Number(value)
  return Number.isSafeInteger(n) && n > 0 ? n : null
}

/** Reads the org/project a deep link (e.g. from an alert message) asks the dashboard to select. */
export function readWorkspaceFromSearch(search: string): { org: number | null; project: number | null } {
  const params = new URLSearchParams(search)
  return { org: positiveInt(params.get('org')), project: positiveInt(params.get('project')) }
}

/** The query string without org/project (other params kept); '' when nothing remains. */
export function stripWorkspaceParams(search: string): string {
  const params = new URLSearchParams(search)
  params.delete('org')
  params.delete('project')
  const rest = params.toString()
  return rest ? `?${rest}` : ''
}
