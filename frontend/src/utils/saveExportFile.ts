/**
 * Triggers a client-side file download for a Blob.
 * Uses a hidden anchor tag with object URL; automatically revokes the URL after 60 seconds.
 */
export async function saveExportFile(blob: Blob, filename: string): Promise<void> {
  const url = URL.createObjectURL(blob)
  try {
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.style.display = 'none'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  } finally {
    // Revoke after delay to allow browser / WebView download pipeline to complete
    setTimeout(() => {
      URL.revokeObjectURL(url)
    }, 60_000)
  }
}
