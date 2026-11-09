import type { ReplayChunkDetail } from '../api'
import type { ViewNode } from './model'

export type NodeKind =
  | 'modal'
  | 'toolbar'
  | 'scroll'
  | 'cell'
  | 'section'
  | 'text'
  | 'button'
  | 'image'
  | 'input'
  | 'separator'

/**
 * Container kinds paint chrome (background, border, shadow). Native hierarchies nest many views
 * whose class names share a keyword (`_UIAlertControllerView > _UIAlertBackground > …`,
 * `UINavigationBar > NavigationBarContentView > NavigationBarPlatterContainer`), so a container
 * kind is only applied to the outermost match; nested matches keep native paint order.
 */
const CONTAINER_KINDS: Array<[NodeKind, RegExp]> = [
  ['modal', /Alert|Modal|Dialog|ActionSheet/],
  ['toolbar', /NavigationBar|Toolbar|AppBar|TabBar/],
  ['scroll', /TableView|CollectionView|RecyclerView|ScrollView/],
  ['section', /SectionView|HeaderFooter|SectionHeader/],
  ['cell', /Cell\b|Cell$|Cell<|ItemView|CardView|RowView/],
]

/** Leaf kinds describe what a node is, regardless of its ancestors. */
const LEAF_KINDS: Array<[NodeKind, (cls: string) => boolean]> = [
  ['text', (c) => /(Label|TextView|Text)$/.test(c) && !/TextField|EditText/.test(c)],
  ['input', (c) => /TextField|EditText|TextInput|SearchField/.test(c)],
  ['button', (c) => /Button|Clickable/.test(c)],
  ['image', (c) => /ImageView|Icon$|Image$/.test(c)],
  ['separator', (c) => /Separator/.test(c)],
]

/**
 * Kinds for a node. `inherited` holds the container kinds already applied by an ancestor;
 * pass the returned set (merged with `inherited`) down to children.
 */
export function nodeKinds(cls: string, inherited: ReadonlySet<NodeKind>): Set<NodeKind> {
  const kinds = new Set<NodeKind>()
  for (const [kind, re] of CONTAINER_KINDS) {
    if (!inherited.has(kind) && re.test(cls)) kinds.add(kind)
  }
  for (const [kind, test] of LEAF_KINDS) {
    if (test(cls)) kinds.add(kind)
  }
  return kinds
}

/** Largest scale that fits the root frame inside the box without cropping either axis. */
export function fitScale(root: [number, number, number, number], box: { width: number; height: number }): number {
  const [, , w, h] = root
  if (w <= 0 || h <= 0) return 1
  return Math.min(box.width / w, box.height / h)
}

/**
 * Identity of a chunk set that ignores signed-URL query params (`exp`, `sig` change on every
 * session-detail refetch), so refetching the same session does not reload or reset playback.
 */
export function chunkSignature(chunks: ReplayChunkDetail[]): string {
  return [...chunks]
    .sort((a, b) => a.chunk_seq - b.chunk_seq)
    .map((c) => `${c.chunk_seq}:${c.object_key}:${c.url && c.url.trim() ? 1 : 0}`)
    .join('|')
}

/** Human-readable screen name from a controller class name or mobile route URI. */
export function displayScreenName(raw: string): string {
  let name = raw.trim()
  if (name.startsWith('android://')) {
    name = name.slice('android://'.length)
    if (name.includes('_id_')) {
      name = name.replace(/_id_/g, '{id}')
    }
  } else if (name.startsWith('ios://')) {
    name = name.slice('ios://'.length)
  }
  if (/^UIHostingController</.test(name)) return 'SwiftUI view'
  const generic = name.indexOf('<')
  return generic > 0 ? name.slice(0, generic) : name
}

const CONTENT_LEAF = /Label|TextView|Text$|Button|ImageView|Image$|Icon$|Switch|Slider|TextField|SegmentedControl|Stepper|ProgressView|ActivityIndicator/

/**
 * Whether the tree holds anything a person would recognise (text or a leaf control). SwiftUI
 * screens rendered inside `_UIHostingView` expose only container views to UIKit, so the SDK
 * captures an empty shell for them.
 */
export function hasCapturedContent(tree: ViewNode): boolean {
  const stack: ViewNode[] = [tree]
  while (stack.length > 0) {
    const node = stack.pop()!
    if (typeof node.text === 'string' && node.text.trim()) return true
    const children = node.children ?? []
    if (children.length === 0 && CONTENT_LEAF.test(node.cls ?? '')) return true
    stack.push(...children)
  }
  return false
}
