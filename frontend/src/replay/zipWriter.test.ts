import { describe, expect, it } from 'vitest'
import { computeCrc32, ZipWriter } from './zipWriter'

describe('computeCrc32', () => {
  it('computes standard IEEE 802.3 CRC32 correctly', () => {
    const encoder = new TextEncoder()
    // "123456789" -> standard check value 0xCBF43926
    const data = encoder.encode('123456789')
    expect(computeCrc32(data)).toBe(0xcbf43926)
  })
})

describe('ZipWriter', () => {
  it('builds valid PKZIP archive with STORE method', async () => {
    const zip = new ZipWriter()
    await zip.add({ name: 'hello.txt', data: 'Hello World!', compress: false })
    await zip.add({ name: 'nested/folder/foo.bin', data: new Uint8Array([1, 2, 3, 4]), compress: false })

    const archive = zip.build()
    expect(archive.length).toBeGreaterThan(0)

    const view = new DataView(archive.buffer, archive.byteOffset, archive.byteLength)
    // Check first local header signature
    expect(view.getUint32(0, true)).toBe(0x04034b50)

    // Check EOCD signature at the end
    const eocdSig = view.getUint32(archive.length - 22, true)
    expect(eocdSig).toBe(0x06054b50)

    // Total entries should be 2
    expect(view.getUint16(archive.length - 14, true)).toBe(2)
  })

  it('builds archive with compressed files', async () => {
    const zip = new ZipWriter()
    const repetitiveText = 'A'.repeat(500)
    await zip.add({ name: 'repeat.txt', data: repetitiveText, compress: true })

    const archive = zip.build()
    const view = new DataView(archive.buffer, archive.byteOffset, archive.byteLength)
    expect(view.getUint32(0, true)).toBe(0x04034b50)

    // Compression method at offset 8 should be 8 if deflate-raw is supported, or 0
    const method = view.getUint16(8, true)
    expect([0, 8]).toContain(method)
  })
})
