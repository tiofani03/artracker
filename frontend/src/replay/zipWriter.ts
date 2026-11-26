/**
 * Lightweight, zero-dependency streaming PKZIP writer for browser and web worker environments.
 * Supports STORE (0) and DEFLATE (8) via Web API CompressionStream('deflate-raw').
 */

// CRC-32 lookup table (IEEE 802.3)
const crcTable = new Uint32Array(256)
for (let i = 0; i < 256; i++) {
  let c = i
  for (let k = 0; k < 8; k++) {
    c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  }
  crcTable[i] = c >>> 0
}

export function computeCrc32(data: Uint8Array): number {
  let crc = 0xffffffff
  for (let i = 0; i < data.length; i++) {
    crc = (crc >>> 8) ^ crcTable[(crc ^ data[i]) & 0xff]
  }
  return (crc ^ 0xffffffff) >>> 0
}

export type ZipEntryInput = {
  name: string
  data: Uint8Array | string
  /** If false, uses method 0 (STORE); if true, compresses with method 8 (DEFLATE) if available */
  compress?: boolean
  date?: Date
}

type PreparedFile = {
  nameBytes: Uint8Array
  dataBytes: Uint8Array
  compressedBytes: Uint8Array
  method: number // 0 = STORE, 8 = DEFLATE
  crc32: number
  uncompressedSize: number
  compressedSize: number
  dosTime: number
  dosDate: number
  localHeaderOffset: number
}

function dateToDosDateTime(d: Date): { dosTime: number; dosDate: number } {
  const year = Math.max(1980, d.getFullYear())
  const dosDate = ((year - 1980) << 9) | ((d.getMonth() + 1) << 5) | d.getDate()
  const dosTime = (d.getHours() << 11) | (d.getMinutes() << 5) | (d.getSeconds() >> 1)
  return { dosTime, dosDate }
}

async function compressDeflateRaw(bytes: Uint8Array): Promise<Uint8Array | null> {
  if (typeof CompressionStream === 'undefined') return null
  try {
    const cs = new CompressionStream('deflate-raw')
    const stream = new Response(new Blob([bytes as unknown as BlobPart]).stream().pipeThrough(cs))
    const buf = await stream.arrayBuffer()
    return new Uint8Array(buf)
  } catch {
    return null
  }
}

export class ZipWriter {
  private files: PreparedFile[] = []

  /**
   * Adds a file entry to the zip archive.
   */
  async add(entry: ZipEntryInput): Promise<void> {
    const encoder = new TextEncoder()
    const nameBytes = encoder.encode(entry.name.replace(/\\/g, '/'))
    const dataBytes = typeof entry.data === 'string' ? encoder.encode(entry.data) : entry.data

    const crc = computeCrc32(dataBytes)
    const { dosTime, dosDate } = dateToDosDateTime(entry.date ?? new Date())

    let method = 0
    let compressedBytes = dataBytes

    if (entry.compress) {
      const deflated = await compressDeflateRaw(dataBytes)
      // Only use deflate if it actually produced smaller data
      if (deflated && deflated.length < dataBytes.length) {
        method = 8
        compressedBytes = deflated
      }
    }

    this.files.push({
      nameBytes,
      dataBytes,
      compressedBytes,
      method,
      crc32: crc,
      uncompressedSize: dataBytes.length,
      compressedSize: compressedBytes.length,
      dosTime,
      dosDate,
      localHeaderOffset: 0,
    })
  }

  /**
   * Builds the complete PKZIP archive buffer as Uint8Array.
   */
  build(): Uint8Array {
    const parts: Uint8Array[] = []
    let currentOffset = 0

    // 1. Write Local File Headers and file data
    for (const file of this.files) {
      file.localHeaderOffset = currentOffset

      const localHeader = new Uint8Array(30 + file.nameBytes.length)
      const view = new DataView(localHeader.buffer)

      view.setUint32(0, 0x04034b50, true) // local header signature
      view.setUint16(4, 20, true) // version needed (2.0)
      view.setUint16(6, 0x0800, true) // bit flag: UTF-8 filename (bit 11)
      view.setUint16(8, file.method, true) // compression method (0 or 8)
      view.setUint16(10, file.dosTime, true)
      view.setUint16(12, file.dosDate, true)
      view.setUint32(14, file.crc32, true)
      view.setUint32(18, file.compressedSize, true)
      view.setUint32(22, file.uncompressedSize, true)
      view.setUint16(26, file.nameBytes.length, true)
      view.setUint16(28, 0, true) // extra field length
      localHeader.set(file.nameBytes, 30)

      parts.push(localHeader)
      parts.push(file.compressedBytes)
      currentOffset += localHeader.length + file.compressedBytes.length
    }

    // 2. Write Central Directory Headers
    const centralDirOffset = currentOffset
    let centralDirSize = 0

    for (const file of this.files) {
      const cdHeader = new Uint8Array(46 + file.nameBytes.length)
      const view = new DataView(cdHeader.buffer)

      view.setUint32(0, 0x02014b50, true) // central dir signature
      view.setUint16(4, 20, true) // version made by
      view.setUint16(6, 20, true) // version needed
      view.setUint16(8, 0x0800, true) // bit flag UTF-8
      view.setUint16(10, file.method, true)
      view.setUint16(12, file.dosTime, true)
      view.setUint16(14, file.dosDate, true)
      view.setUint32(16, file.crc32, true)
      view.setUint32(20, file.compressedSize, true)
      view.setUint32(24, file.uncompressedSize, true)
      view.setUint16(28, file.nameBytes.length, true)
      view.setUint16(30, 0, true) // extra field length
      view.setUint16(32, 0, true) // file comment length
      view.setUint16(34, 0, true) // disk number start
      view.setUint16(36, 0, true) // internal file attributes
      view.setUint32(38, 0, true) // external file attributes
      view.setUint32(42, file.localHeaderOffset, true)
      cdHeader.set(file.nameBytes, 46)

      parts.push(cdHeader)
      centralDirSize += cdHeader.length
      currentOffset += cdHeader.length
    }

    // 3. Write End of Central Directory Record (EOCD)
    const eocd = new Uint8Array(22)
    const eocdView = new DataView(eocd.buffer)

    eocdView.setUint32(0, 0x06054b50, true) // EOCD signature
    eocdView.setUint16(4, 0, true) // number of this disk
    eocdView.setUint16(6, 0, true) // disk with central dir
    eocdView.setUint16(8, this.files.length, true) // total entries on this disk
    eocdView.setUint16(10, this.files.length, true) // total entries
    eocdView.setUint32(12, centralDirSize, true) // size of central dir
    eocdView.setUint32(16, centralDirOffset, true) // offset of start of central dir
    eocdView.setUint16(20, 0, true) // comment length

    parts.push(eocd)
    currentOffset += eocd.length

    // Concat all parts into single Uint8Array
    const result = new Uint8Array(currentOffset)
    let pos = 0
    for (const part of parts) {
      result.set(part, pos)
      pos += part.length
    }

    return result
  }
}
