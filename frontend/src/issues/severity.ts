// Go twin: backend/internal/issues/severity.go (IsFatal); keep rules in sync.
import type { Issue, IssueStatus } from '../api'

/** Status wording shared across the Issues pages. */
export const STATUS_LABEL: Record<IssueStatus, string> = {
  open: 'Open',
  resolved: 'Closed',
  ignored: 'Muted',
}

/** Shared by the issue list and detail pages so both label severity the same way. */
export function isFatalCrash(issue: Issue): boolean {
  const type = (issue.type || '').toLowerCase()
  const title = (issue.title || '').toLowerCase()
  return (
    type.includes('fatal') ||
    type.includes('crash') ||
    type.includes('panic') ||
    title.includes('uncaught') ||
    title.includes('unhandled') ||
    title.includes('fatal') ||
    title.includes('crash') ||
    type.includes('typeerror') ||
    type.includes('referenceerror')
  )
}
