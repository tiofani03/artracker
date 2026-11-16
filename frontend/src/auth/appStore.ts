// Persistent storage for the native app. The refresh token lives in the app's private
// data directory (no cookie jar across tauri:// and the API origin).
const FILE = 'auth.json'

async function store() {
  const { load } = await import('@tauri-apps/plugin-store')
  return load(FILE, { autoSave: true, defaults: {} })
}

export async function getStored(key: string): Promise<string | null> {
  try {
    return ((await (await store()).get<string>(key)) as string | undefined) ?? null
  } catch {
    return null
  }
}

export async function setStored(key: string, value: string | null): Promise<void> {
  try {
    const s = await store()
    if (value === null) await s.delete(key)
    else await s.set(key, value)
    await s.save()
  } catch {
    // storage unavailable: the user just has to sign in again next launch
  }
}

export const getRefreshToken = () => getStored('refresh_token')
export const setRefreshToken = (t: string | null) => setStored('refresh_token', t)
