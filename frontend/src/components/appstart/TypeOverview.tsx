import type { AppStartType, AppStartTypeStats } from '../../api'
import { formatMs, shareLabel } from '../../appstart/format'

const TYPES: { type: AppStartType; label: string; desc: string }[] = [
  { type: 'cold', label: 'Cold', desc: 'App process started from scratch' },
  { type: 'warm', label: 'Warm', desc: 'Process alive, screen recreated' },
  { type: 'hot', label: 'Hot', desc: 'App brought back to the foreground' },
]

/** Every start type side by side under the page filters; picking one drives the rest of the page. */
export default function TypeOverview({
  stats,
  selected,
  onSelect,
}: {
  stats: AppStartTypeStats[]
  selected: AppStartType
  onSelect: (t: AppStartType) => void
}) {
  const byType = new Map(stats.map((s) => [s.type, s]))
  const total = stats.reduce((n, s) => n + s.count, 0)
  return (
    <section className="dash-card as-types" aria-label="All start types">
      <div className="as-types__head">
        <h2 className="dash-card__title">All start types</h2>
        <span className="as-types__total">{total.toLocaleString()} starts</span>
      </div>
      <div className="as-types__grid">
        {TYPES.map(({ type, label, desc }) => {
          const s = byType.get(type)
          const count = s?.count ?? 0
          const share = total ? (100 * count) / total : 0
          return (
            <button
              key={type}
              type="button"
              className={`as-types__tile${type === selected ? ' is-active' : ''}`}
              aria-pressed={type === selected}
              onClick={() => onSelect(type)}
            >
              <span className="as-types__row">
                <span className="as-types__name">{label}</span>
                <span className="as-types__count">
                  {count.toLocaleString()} · {shareLabel(count, total)}
                </span>
              </span>
              <span className="as-types__bar" aria-hidden="true">
                <span style={{ width: `${share}%` }} />
              </span>
              <span className="as-types__desc">{desc}</span>
              <span className="as-types__pcts">
                <span>
                  p50 <strong>{count ? formatMs(s!.p50) : '—'}</strong>
                </span>
                <span>
                  p90 <strong>{count ? formatMs(s!.p90) : '—'}</strong>
                </span>
              </span>
            </button>
          )
        })}
      </div>
    </section>
  )
}
