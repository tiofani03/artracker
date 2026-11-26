import { describe, expect, it, vi } from 'vitest'
import { saveExportFile } from './saveExportFile'

describe('saveExportFile', () => {
  it('creates an object URL, clicks a temporary anchor tag, and schedules revocation', async () => {
    const createObjectURLMock = vi.fn().mockReturnValue('blob:http://localhost/mock-uuid')
    const revokeObjectURLMock = vi.fn()
    globalThis.URL.createObjectURL = createObjectURLMock
    globalThis.URL.revokeObjectURL = revokeObjectURLMock

    let clicked = false
    let downloadAttr = ''
    let hrefAttr = ''
    let appended = false
    let removed = false

    const mockAnchor = {
      download: '',
      href: '',
      style: { display: '' },
      click: () => {
        clicked = true
        downloadAttr = mockAnchor.download
        hrefAttr = mockAnchor.href
      },
    }

    const mockDoc = {
      createElement: (tag: string) => (tag === 'a' ? mockAnchor : {}),
      body: {
        appendChild: () => {
          appended = true
        },
        removeChild: () => {
          removed = true
        },
      },
    }

    const origDoc = globalThis.document
    // @ts-expect-error mock document in node test environment
    globalThis.document = mockDoc

    try {
      const blob = new Blob(['sample data'], { type: 'text/plain' })
      await saveExportFile(blob, 'export-test.txt')

      expect(createObjectURLMock).toHaveBeenCalledWith(blob)
      expect(clicked).toBe(true)
      expect(appended).toBe(true)
      expect(removed).toBe(true)
      expect(downloadAttr).toBe('export-test.txt')
      expect(hrefAttr).toBe('blob:http://localhost/mock-uuid')
    } finally {
      globalThis.document = origDoc
    }
  })
})
