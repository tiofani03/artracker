import { useCallback, useEffect, useState } from 'react'
import { api, type Artwork } from './api'

type FormState = { title: string; artist: string; year: string; medium: string }

const emptyForm: FormState = { title: '', artist: '', year: '', medium: '' }

export default function App() {
  const [items, setItems] = useState<Artwork[]>([])
  const [form, setForm] = useState<FormState>(emptyForm)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const data = await api.list()
      setItems(data.items)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'gagal memuat data')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault()
    if (!form.title.trim()) return

    setSaving(true)
    setError(null)
    try {
      const year = form.year.trim() === '' ? null : Number(form.year)
      await api.create({
        title: form.title.trim(),
        artist: form.artist.trim(),
        medium: form.medium.trim(),
        year: year !== null && Number.isFinite(year) ? year : null,
      })
      setForm(emptyForm)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'gagal menyimpan')
    } finally {
      setSaving(false)
    }
  }

  async function onDelete(id: number) {
    try {
      await api.remove(id)
      setItems((current) => current.filter((item) => item.id !== id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'gagal menghapus')
    }
  }

  return (
    <main className="shell">
      <header>
        <h1>Art Tracker</h1>
        <p className="muted">{items.length} karya tercatat</p>
      </header>

      <form className="card form" onSubmit={onSubmit}>
        <div className="grid">
          <label>
            Judul <span className="req">*</span>
            <input
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
              placeholder="Judul karya"
              required
            />
          </label>
          <label>
            Seniman
            <input
              value={form.artist}
              onChange={(e) => setForm({ ...form, artist: e.target.value })}
              placeholder="Nama seniman"
            />
          </label>
          <label>
            Tahun
            <input
              value={form.year}
              onChange={(e) => setForm({ ...form, year: e.target.value })}
              placeholder="2026"
              inputMode="numeric"
            />
          </label>
          <label>
            Medium
            <input
              value={form.medium}
              onChange={(e) => setForm({ ...form, medium: e.target.value })}
              placeholder="Kanvas, akrilik"
            />
          </label>
        </div>
        <button type="submit" disabled={saving}>
          {saving ? 'Menyimpan…' : 'Tambah karya'}
        </button>
      </form>

      {error && <p className="error">{error}</p>}

      {loading ? (
        <p className="muted">Memuat…</p>
      ) : items.length === 0 ? (
        <p className="muted">Belum ada karya. Tambah yang pertama.</p>
      ) : (
        <ul className="list">
          {items.map((item) => (
            <li key={item.id} className="card item">
              <div>
                <h2>{item.title}</h2>
                <p className="muted">
                  {[item.artist, item.year, item.medium].filter(Boolean).join(' · ') || '—'}
                </p>
              </div>
              <button className="ghost" onClick={() => void onDelete(item.id)}>
                Hapus
              </button>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}
