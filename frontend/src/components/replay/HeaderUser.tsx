import { peopleLink } from '../../people/format'
import { Link } from '../../router'

/** The user id in a session header; links to the user's People profile when the session is identified. */
export function HeaderUser({ userId }: { userId: string }) {
  if (!userId) {
    return (
      <strong className="or-unified-header__user" title="anonymous_user">
        anonymous_user
      </strong>
    )
  }
  return (
    <strong className="or-unified-header__user" title={`${userId} — view profile`}>
      <Link to={peopleLink(userId)} className="or-unified-header__user-link">
        {userId}
      </Link>
    </strong>
  )
}
