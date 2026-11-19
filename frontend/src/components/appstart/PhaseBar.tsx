import { formatMs, phaseLabel } from '../../appstart/format'

type Phase = { name: string; ms: number }

const TONES = 6

/** Stacked horizontal bar of start phases (widths proportional to `total`) with a labelled legend underneath. */
export default function PhaseBar({ phases, total }: { phases: Phase[]; total: number }) {
  const shown = phases.filter((p) => p.ms > 0)
  const denom = Math.max(total, shown.reduce((sum, p) => sum + p.ms, 0))
  if (shown.length === 0 || denom <= 0) return <p className="ppl-muted">No phase timings were reported.</p>
  return (
    <div className="as-phase">
      <div className="as-phase__bar" role="img" aria-label="Start phases">
        {shown.map((p, i) => (
          <span
            key={p.name}
            className={`as-phase__seg as-phase__seg--${i % TONES}`}
            style={{ width: `${(p.ms / denom) * 100}%` }}
            title={`${phaseLabel(p.name)}: ${formatMs(p.ms)}`}
          />
        ))}
      </div>
      <ul className="as-phase__legend">
        {shown.map((p, i) => (
          <li key={p.name} className="as-phase__item">
            <span className={`as-phase__swatch as-phase__seg--${i % TONES}`} aria-hidden="true" />
            <span className="as-phase__name">{phaseLabel(p.name)}</span>
            <span className="as-phase__ms">{formatMs(p.ms)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
