/**
 * Compresses a UTF-8 string into GZIP bytes using the standard Web Streams CompressionStream API.
 */
export async function gzipString(input: string): Promise<Uint8Array> {
  const encoded = new TextEncoder().encode(input)
  if (typeof CompressionStream === 'undefined') {
    throw new Error('CompressionStream("gzip") is not supported in this environment')
  }
  const cs = new CompressionStream('gzip')
  const writer = cs.writable.getWriter()
  void writer.write(encoded)
  void writer.close()

  const buf = await new Response(cs.readable).arrayBuffer()
  return new Uint8Array(buf)
}

/**
 * Decompresses GZIP bytes back to a UTF-8 string (auto-detects GZIP magic header 0x1f 0x8b
 * so plain UTF-8 payloads transparently pass through if already decompressed by transport).
 */
export async function gunzipToString(data: ArrayBuffer | Uint8Array): Promise<string> {
  const bytes = data instanceof Uint8Array ? data : new Uint8Array(data)
  if (bytes.byteLength >= 2 && bytes[0] === 0x1f && bytes[1] === 0x8b) {
    const ds = new DecompressionStream('gzip')
    const writer = ds.writable.getWriter()
    void writer.write(bytes as unknown as BufferSource)
    void writer.close()
    const out = await new Response(ds.readable).arrayBuffer()
    return new TextDecoder().decode(out)
  }
  return new TextDecoder().decode(bytes)
}
