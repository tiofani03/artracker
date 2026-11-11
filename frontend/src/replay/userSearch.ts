/** Longest user id the ingest pipeline keeps, and so the longest useful search. */
export const USER_QUERY_MAX = 128

/** What is sent as `user_prefix`: trimmed and capped; empty means "no search". */
export function normalizeUserQuery(raw: string): string {
  return raw.trim().slice(0, USER_QUERY_MAX)
}
