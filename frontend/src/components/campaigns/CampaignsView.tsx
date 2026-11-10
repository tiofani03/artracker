import { useEffect, useMemo, useState } from 'react'
import {
  analyticsApi,
  type Project,
} from '../../api'
import CustomSelect, { type CustomSelectOption } from '../CustomSelect'

type Props = {
  project: Project | null
  isDemo?: boolean
}

type CampaignRow = {
  id: string
  name: string
  source: string
  channel: string
  visitors: number
  signups: number
  addToCart: number
  checkoutStart: number
  checkouts: number
  conversionRate: number
  status: 'Top Performer' | 'Healthy' | 'Low Conversion'
}

type RangeOption = '7d' | '30d' | '90d'

export default function CampaignsView({ project }: Props) {
  const [range, setRange] = useState<RangeOption>('30d')
  const [channelFilter, setChannelFilter] = useState('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [campaigns, setCampaigns] = useState<CampaignRow[]>([])
  const [selectedCampaign, setSelectedCampaign] = useState<CampaignRow | null>(null)
  const [loadingFunnel, setLoadingFunnel] = useState(false)

  // UTM Generator State
  const [targetUrl, setTargetUrl] = useState('https://example.com/pricing')
  const [utmSource, setUtmSource] = useState('newsletter')
  const [utmMedium, setUtmMedium] = useState('email')
  const [utmCampaign, setUtmCampaign] = useState('black_friday_2026')
  const [copied, setCopied] = useState(false)

  // Load real campaigns from backend breakdown
  useEffect(() => {
    if (!project) return
    let active = true
    const targetPid = project.id
    const days = range === '7d' ? 7 : range === '90d' ? 90 : 30
    const from = new Date(Date.now() - days * 86400 * 1000).toISOString()
    const to = new Date().toISOString()

    analyticsApi
      .calculateFunnel(targetPid, {
        steps: ['landing_view', 'view_product', 'add_to_cart', 'checkout_start', 'purchase_completed'],
        from,
        to,
        window_seconds: 86400,
        counting_mode: 'users',
        breakdown_by: 'utm_source',
      })
      .then((res) => {
        if (!active) return
        if (!res.breakdowns || res.breakdowns.length === 0) {
          setCampaigns([])
          setSelectedCampaign(null)
          return
        }
        const mapped: CampaignRow[] = res.breakdowns
          .filter((bd) => (bd.step_counts[0] || 0) > 0)
          .map((bd, i) => {
            const visitors = bd.step_counts[0] || 0
            const signups = bd.step_counts[1] || 0
            const addToCart = bd.step_counts[2] || 0
            const checkoutStart = bd.step_counts[3] || 0
            const checkouts = bd.step_counts[4] || 0
            const rate = visitors > 0 ? (checkouts / visitors) * 100 : 0
            const seg = bd.segment && bd.segment !== '(none)' ? bd.segment : 'organic'
            const channelName = seg.charAt(0).toUpperCase() + seg.slice(1)
            const status: CampaignRow['status'] =
              rate >= 18 ? 'Top Performer' : rate >= 12 ? 'Healthy' : 'Low Conversion'

            return {
              id: 'real-c-' + i,
              name: seg.startsWith('utm_') ? seg : `utm_${seg}`,
              source: `${seg} / campaign`,
              channel: channelName,
              visitors,
              signups,
              addToCart,
              checkoutStart,
              checkouts,
              conversionRate: Number(rate.toFixed(2)),
              status,
            }
          })
        setCampaigns(mapped)
      })
      .catch(() => {
        if (active) setCampaigns([])
      })

    return () => {
      active = false
    }
  }, [project, range])

  // Filtered campaigns
  const filteredCampaigns = useMemo(() => {
    return campaigns.filter((c) => {
      const matchChannel =
        channelFilter === 'all' ||
        c.channel.toLowerCase() === channelFilter.toLowerCase()
      const matchQuery =
        !searchQuery.trim() ||
        c.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        c.source.toLowerCase().includes(searchQuery.toLowerCase()) ||
        c.channel.toLowerCase().includes(searchQuery.toLowerCase())
      return matchChannel && matchQuery
    })
  }, [campaigns, channelFilter, searchQuery])

  // Summary Metrics
  const summary = useMemo(() => {
    const totalVisitors = campaigns.reduce((sum, c) => sum + c.visitors, 0)
    const totalSignups = campaigns.reduce((sum, c) => sum + c.signups, 0)
    const totalCheckouts = campaigns.reduce((sum, c) => sum + c.checkouts, 0)
    const avgConversion =
      totalVisitors > 0 ? (totalCheckouts / totalVisitors) * 100 : 0

    const bestCampaign = [...campaigns].sort(
      (a, b) => b.conversionRate - a.conversionRate,
    )[0]

    return {
      totalVisitors,
      totalSignups,
      totalCheckouts,
      avgConversion: avgConversion.toFixed(2),
      bestCampaign,
    }
  }, [campaigns])

  // When a campaign is selected, simulate short feedback
  useEffect(() => {
    if (!selectedCampaign) return
    setLoadingFunnel(true)

    const timer = setTimeout(() => {
      setLoadingFunnel(false)
    }, 120)

    return () => clearTimeout(timer)
  }, [selectedCampaign])

  // Channel dropdown options
  const channelOptions: CustomSelectOption<string>[] = [
    { value: 'all', label: 'All Marketing Channels' },
    { value: 'email', label: 'Email Newsletters' },
    { value: 'google', label: 'Google Search & CPC' },
    { value: 'instagram', label: 'Instagram Ads' },
    { value: 'twitter', label: 'Twitter / X' },
    { value: 'github', label: 'GitHub Referral' },
  ]

  // Generated tracking URL
  const generatedUrl = useMemo(() => {
    const base = targetUrl.trim() || 'https://example.com'
    const params = new URLSearchParams()
    if (utmSource.trim()) params.set('utm_source', utmSource.trim())
    if (utmMedium.trim()) params.set('utm_medium', utmMedium.trim())
    if (utmCampaign.trim()) params.set('utm_campaign', utmCampaign.trim())
    const q = params.toString()
    return q ? `${base}${base.includes('?') ? '&' : '?'}${q}` : base
  }, [targetUrl, utmSource, utmMedium, utmCampaign])

  const handleCopyUrl = async () => {
    try {
      await navigator.clipboard.writeText(generatedUrl)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // fallback
    }
  }

  return (
    <div className="campaigns-view-container">
      {/* Top Header Card */}
      <div className="dash-card dash-card--compact campaigns-header-card">
        <div className="campaigns-header-main">
          <div>
            <h2 className="dash-card__title">Marketing Campaigns &amp; Attribution</h2>
            <p className="dash-card__desc">
              Track visitor acquisition, conversion rates, and checkout ROI across all marketing links.
            </p>
          </div>

          <div className="campaigns-header-actions">
            {/* Range Toggle */}
            <div className="dash-seg" role="group" aria-label="Date range">
              {(['7d', '30d', '90d'] as RangeOption[]).map((r) => (
                <button
                  key={r}
                  type="button"
                  className={`dash-seg__btn${range === r ? ' is-active' : ''}`}
                  onClick={() => setRange(r)}
                >
                  {r === '7d' ? '7 Days' : r === '30d' ? '30 Days' : '90 Days'}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* 4 Scorecard Metrics */}
      <div className="campaigns-kpi-grid">
        <div className="campaigns-kpi-card">
          <span className="campaigns-kpi-label">Total Campaign Visitors</span>
          <div className="campaigns-kpi-value">
            {summary.totalVisitors.toLocaleString()}
          </div>
          <span className="campaigns-kpi-sub">
            Tracked across all inbound UTM parameters
          </span>
        </div>

        <div className="campaigns-kpi-card">
          <span className="campaigns-kpi-label">Account Signups</span>
          <div className="campaigns-kpi-value">
            {summary.totalSignups.toLocaleString()}
          </div>
          <span className="campaigns-kpi-sub">
            {summary.totalVisitors > 0
              ? `${((summary.totalSignups / summary.totalVisitors) * 100).toFixed(1)}%`
              : '0%'} visitor-to-signup rate
          </span>
        </div>

        <div className="campaigns-kpi-card campaigns-kpi-card--primary">
          <span className="campaigns-kpi-label">Completed Purchases</span>
          <div className="campaigns-kpi-value">
            {summary.totalCheckouts.toLocaleString()}
          </div>
          <span className="campaigns-kpi-sub">
            Avg. conversion rate: {summary.avgConversion}%
          </span>
        </div>

        <div className="campaigns-kpi-card">
          <span className="campaigns-kpi-label">Top Performing Campaign</span>
          <div className="campaigns-kpi-value campaigns-kpi-value--truncate">
            {summary.bestCampaign?.name || 'None'}
          </div>
          <span className="campaigns-kpi-sub">
            {summary.bestCampaign
              ? `${summary.bestCampaign.conversionRate}% conv. (${summary.bestCampaign.channel})`
              : 'No active campaigns'}
          </span>
        </div>
      </div>

      {/* Campaigns Table Section */}
      <section className="dash-card dash-card--compact">
        <div className="campaigns-table-toolbar">
          <div className="campaigns-table-search-wrap">
            <input
              type="text"
              className="dash-input campaigns-search-input"
              placeholder="Search campaign name or source..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              aria-label="Filter campaigns by name"
            />
          </div>

          <div className="campaigns-table-channel-filter">
            <CustomSelect
              size="sm"
              options={channelOptions}
              value={channelFilter}
              onChange={setChannelFilter}
              ariaLabel="Filter by marketing channel"
            />
          </div>
        </div>

        <div className="dash-table-wrap">
          <table className="dash-table">
            <thead>
              <tr>
                <th>Campaign Name</th>
                <th>Channel</th>
                <th style={{ textAlign: 'right' }}>Visitors</th>
                <th style={{ textAlign: 'right' }}>Signups</th>
                <th style={{ textAlign: 'right' }}>Checkouts</th>
                <th>Conversion</th>
                <th>Status</th>
                <th style={{ textAlign: 'right' }}>Action</th>
              </tr>
            </thead>
            <tbody>
              {filteredCampaigns.length === 0 ? (
                <tr>
                  <td colSpan={8} className="campaigns-empty-row">
                    No marketing campaigns found matching "{searchQuery}".
                  </td>
                </tr>
              ) : (
                filteredCampaigns.map((c) => {
                  const isSelected = selectedCampaign?.id === c.id

                  return (
                    <tr
                      key={c.id}
                      className={isSelected ? 'campaigns-row-selected' : ''}
                    >
                      <td data-label="Campaign">
                        <strong className="dash-code">{c.name}</strong>
                        <div className="dash-cell-sub">{c.source}</div>
                      </td>
                      <td data-label="Channel">
                        <span className="campaigns-channel-tag">{c.channel}</span>
                      </td>
                      <td
                        data-label="Visitors"
                        style={{ textAlign: 'right', fontFamily: 'var(--font-mono)' }}
                      >
                        {c.visitors.toLocaleString()}
                      </td>
                      <td
                        data-label="Signups"
                        style={{ textAlign: 'right', fontFamily: 'var(--font-mono)' }}
                      >
                        {c.signups.toLocaleString()}
                      </td>
                      <td
                        data-label="Checkouts"
                        style={{ textAlign: 'right', fontFamily: 'var(--font-mono)' }}
                      >
                        {c.checkouts.toLocaleString()}
                      </td>
                      <td data-label="Conversion">
                        <span className="badge badge--role">
                          {c.conversionRate.toFixed(2)}%
                        </span>
                      </td>
                      <td data-label="Status">
                        <span
                          className={`campaigns-status-tag campaigns-status-tag--${
                            c.status === 'Top Performer'
                              ? 'top'
                              : c.status === 'Healthy'
                                ? 'healthy'
                                : 'low'
                          }`}
                        >
                          {c.status}
                        </span>
                      </td>
                      <td data-label="Action" style={{ textAlign: 'right' }}>
                        <button
                          type="button"
                          className="dash-btn dash-btn--sm"
                          onClick={() =>
                            setSelectedCampaign(isSelected ? null : c)
                          }
                        >
                          {isSelected ? 'Close Journey' : 'Inspect Journey'}
                        </button>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* Selected Campaign Journey Inspection (Instant, No Builder Hassle!) */}
      {selectedCampaign && (
        <section className="dash-card dash-card--compact campaigns-journey-card">
          <div className="campaigns-journey-header">
            <div>
              <span className="campaigns-journey-tag">
                Campaign Attribution Journey
              </span>
              <h3 className="campaigns-journey-title">
                {selectedCampaign.name} ({selectedCampaign.channel})
              </h3>
              <p className="campaigns-journey-desc">
                Step-by-step conversion drop-offs for visitors originating from {selectedCampaign.source}.
              </p>
            </div>
            <button
              type="button"
              className="dash-btn dash-btn--sm"
              onClick={() => setSelectedCampaign(null)}
            >
              Close Journey
            </button>
          </div>

          {loadingFunnel ? (
            <div className="dash-empty-state">
              <p>Computing campaign journey...</p>
            </div>
          ) : (
            <div className="campaigns-journey-steps">
              {[
                {
                  label: 'Landing Page View',
                  event: 'landing_view',
                  count: selectedCampaign.visitors,
                  pct: 100,
                },
                {
                  label: 'Product Detail View',
                  event: 'view_product',
                  count: selectedCampaign.signups,
                  pct: selectedCampaign.visitors > 0 ? (selectedCampaign.signups / selectedCampaign.visitors) * 100 : 0,
                },
                {
                  label: 'Add to Cart',
                  event: 'add_to_cart',
                  count: selectedCampaign.addToCart,
                  pct: selectedCampaign.visitors > 0 ? (selectedCampaign.addToCart / selectedCampaign.visitors) * 100 : 0,
                },
                {
                  label: 'Begin Checkout',
                  event: 'checkout_start',
                  count: selectedCampaign.checkoutStart,
                  pct: selectedCampaign.visitors > 0 ? (selectedCampaign.checkoutStart / selectedCampaign.visitors) * 100 : 0,
                },
                {
                  label: 'Purchase Completed',
                  event: 'purchase_completed',
                  count: selectedCampaign.checkouts,
                  pct: selectedCampaign.conversionRate,
                },
              ].map((st, idx, arr) => {
                const isFinal = idx === arr.length - 1
                return (
                  <div key={st.event} className="campaigns-journey-step-item">
                    <div className="campaigns-journey-step-bar-wrap">
                      <div className="campaigns-journey-step-head">
                        <span className="campaigns-journey-step-badge">Step {idx + 1}</span>
                        <strong>{st.label}</strong>
                        <span className="campaigns-journey-step-count">
                          {st.count.toLocaleString()} users ({st.pct.toFixed(1)}%)
                        </span>
                      </div>
                      <div className="campaigns-journey-track">
                        <div
                          className={`campaigns-journey-fill${isFinal ? ' campaigns-journey-fill--final' : ''}`}
                          style={{ width: `${Math.max(st.pct, 5)}%` }}
                        />
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </section>
      )}

      {/* Marketer's Quick UTM Tracking Link Generator */}
      <section className="dash-card dash-card--compact campaigns-utm-generator">
        <div className="campaigns-utm-header">
          <h3 className="campaigns-utm-title">UTM Campaign URL Builder</h3>
          <p className="campaigns-utm-desc">
            Generate clean tracking URLs for your email newsletters, paid ads, and social media posts.
          </p>
        </div>

        <div className="campaigns-utm-form-grid">
          <div className="campaigns-utm-field">
            <label htmlFor="utm-target-url" className="campaigns-utm-label">
              Destination Website URL:
            </label>
            <input
              id="utm-target-url"
              type="url"
              className="dash-input"
              value={targetUrl}
              onChange={(e) => setTargetUrl(e.target.value)}
              placeholder="https://yourdomain.com/landing"
            />
          </div>

          <div className="campaigns-utm-field">
            <label htmlFor="utm-source" className="campaigns-utm-label">
              Campaign Source (utm_source):
            </label>
            <input
              id="utm-source"
              type="text"
              className="dash-input"
              value={utmSource}
              onChange={(e) => setUtmSource(e.target.value)}
              placeholder="e.g. google, newsletter, twitter"
            />
          </div>

          <div className="campaigns-utm-field">
            <label htmlFor="utm-medium" className="campaigns-utm-label">
              Campaign Medium (utm_medium):
            </label>
            <input
              id="utm-medium"
              type="text"
              className="dash-input"
              value={utmMedium}
              onChange={(e) => setUtmMedium(e.target.value)}
              placeholder="e.g. cpc, email, banner"
            />
          </div>

          <div className="campaigns-utm-field">
            <label htmlFor="utm-campaign-name" className="campaigns-utm-label">
              Campaign Name (utm_campaign):
            </label>
            <input
              id="utm-campaign-name"
              type="text"
              className="dash-input"
              value={utmCampaign}
              onChange={(e) => setUtmCampaign(e.target.value)}
              placeholder="e.g. spring_sale, onboarding_q4"
            />
          </div>
        </div>

        <div className="campaigns-utm-result">
          <span className="campaigns-utm-result-label">Generated Tracking URL:</span>
          <div className="campaigns-utm-result-box">
            <code className="campaigns-utm-url-text">{generatedUrl}</code>
            <button
              type="button"
              className="dash-btn dash-btn--primary"
              onClick={handleCopyUrl}
            >
              {copied ? 'Copied to Clipboard' : 'Copy Tracking Link'}
            </button>
          </div>
        </div>
      </section>
    </div>
  )
}
