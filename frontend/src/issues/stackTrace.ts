// Go twin: backend/internal/issues/stackframes.go (keep rules in sync).
/** One line of a stack trace, split for display. */
export type StackFrame = {
  raw: string
  symbol: string
  location: string
  /** True for the app's own code; false for framework / runtime / library frames. */
  app: boolean
}

/** The thrown exception or one of its "Caused by" causes, with its frames. */
export type StackSection = {
  title: string
  causedBy: boolean
  frames: StackFrame[]
}

export type FrameGroup = { kind: 'app'; frame: StackFrame } | { kind: 'library'; frames: StackFrame[] }

// Frames from these runtimes and SDKs are noise when hunting the bug.
const LIBRARY_PREFIXES = [
  'android.',
  'androidx.',
  'com.android.',
  'com.google.android.',
  'dalvik.',
  'java.',
  'javax.',
  'jdk.',
  'kotlin.',
  'kotlinx.',
  'sun.',
  'libcore.',
  'okhttp3.',
  'retrofit2.',
  'io.reactivex.',
]

const LIBRARY_PATTERNS = [
  /node_modules\//,
  /^\.\.\. \d+ more$/,
  /^<anonymous>|^native$/,
  /\b(UIKitCore|UIKit|CoreFoundation|Foundation|libdispatch\.dylib|libsystem_\w+\.dylib|GraphicsServices|libdyld\.dylib)\b/,
]

const CAUSED_BY = /^Caused by:\s*/

function isLibrary(symbol: string, line: string): boolean {
  return LIBRARY_PREFIXES.some((p) => symbol.startsWith(p)) || LIBRARY_PATTERNS.some((re) => re.test(line))
}

function parseFrame(line: string): StackFrame {
  const text = line.trim().replace(/^at\s+/, '')
  // JVM: com.pkg.Class.method(File.kt:12)
  // JS:  fn (file.js:1:2)
  const m = /^(.*?)\s*\(([^()]*)\)$/.exec(text)
  // JS frames without a function name: at file.js:1:2
  const symbol = m ? m[1].trim() : text
  const location = m ? m[2] : ''
  return { raw: line, symbol: symbol || location, location, app: !isLibrary(symbol, text) }
}

/** Splits a raw stack trace into the exception and its causes, each with frames. */
export function parseStackTrace(trace: string): StackSection[] {
  const sections: StackSection[] = []
  for (const line of trace.split('\n')) {
    if (!line.trim()) continue
    const trimmed = line.trim()
    const isFrame = /^at\s/.test(trimmed) || /^\.\.\. \d+ more$/.test(trimmed)
    if (sections.length === 0 || (!isFrame && CAUSED_BY.test(trimmed))) {
      sections.push({
        title: trimmed.replace(CAUSED_BY, ''),
        causedBy: CAUSED_BY.test(trimmed),
        frames: [],
      })
    } else if (isFrame) {
      sections[sections.length - 1].frames.push(parseFrame(line))
    } else {
      // Multi-line exception message: keep it with the title.
      const cur = sections[sections.length - 1]
      if (cur.frames.length === 0) cur.title += `\n${trimmed}`
      else cur.frames.push(parseFrame(line))
    }
  }
  return sections
}

/** Keeps app frames individual and collapses each run of library frames. */
export function groupFrames(frames: StackFrame[]): FrameGroup[] {
  const groups: FrameGroup[] = []
  for (const frame of frames) {
    if (frame.app) {
      groups.push({ kind: 'app', frame })
      continue
    }
    const last = groups[groups.length - 1]
    if (last?.kind === 'library') last.frames.push(frame)
    else groups.push({ kind: 'library', frames: [frame] })
  }
  return groups
}
