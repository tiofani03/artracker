import { describe, expect, it } from 'vitest'
import type { ReplayChunkDetail } from '../api'
import type { ViewNode } from './model'
import { chunkSignature, displayScreenName, fitScale, hasCapturedContent, nodeKinds } from './wireframe'

describe('nodeKinds', () => {
  it('styles only the outermost alert container as a modal (real iOS alert hierarchy)', () => {
    const outer = nodeKinds('_UIAlertControllerPhoneTVMacView', new Set())
    expect(outer.has('modal')).toBe(true)
    // nested alert parts must not repaint an opaque, elevated modal over the labels
    expect(nodeKinds('_UIAlertBackground', outer).has('modal')).toBe(false)
    expect(nodeKinds('_UIAlertControllerInterfaceActionGroupView', outer).has('modal')).toBe(false)
  })

  it('styles only the outermost navigation bar as a toolbar', () => {
    const bar = nodeKinds('UINavigationBar', new Set())
    expect(bar.has('toolbar')).toBe(true)
    expect(nodeKinds('NavigationBarPlatterContainer', bar).has('toolbar')).toBe(false)
    expect(nodeKinds('NavigationBarContentView', bar).has('toolbar')).toBe(false)
  })

  it('still classifies leaf kinds inside a styled container', () => {
    const bar = nodeKinds('UINavigationBar', new Set())
    expect(nodeKinds('UILabel', bar).has('text')).toBe(true)
    expect(nodeKinds('UIButton', bar).has('button')).toBe(true)
  })

  it('does not treat text fields or label containers as plain text', () => {
    expect(nodeKinds('UISearchBarTextField', new Set()).has('text')).toBe(false)
    expect(nodeKinds('WHLabel', new Set()).has('text')).toBe(true)
  })
})

describe('fitScale', () => {
  it('fits a tall iPhone root inside both width and height limits (no cropping)', () => {
    const s = fitScale([0, 0, 402, 874], { width: 308, height: 520 })
    expect(s).toBeCloseTo(520 / 874, 5)
    expect(874 * s).toBeLessThanOrEqual(520)
  })

  it('uses the width ratio when height is not the constraint', () => {
    expect(fitScale([0, 0, 402, 874], { width: 308, height: 2000 })).toBeCloseTo(308 / 402, 5)
  })

  it('returns 1 for a degenerate root', () => {
    expect(fitScale([0, 0, 0, 0], { width: 308, height: 520 })).toBe(1)
  })
})

describe('chunkSignature', () => {
  const c = (seq: number, url: string): ReplayChunkDetail => ({
    chunk_seq: seq,
    object_key: `projects/4/sessions/s/chunk_${seq}.gz`,
    size_bytes: 10,
    event_count: 1,
    created_at: '2026-11-09T00:00:00Z',
    url,
  })

  it('ignores signed URL query changes between refetches of the same chunks', () => {
    const a = chunkSignature([c(0, '/d?exp=1&sig=a'), c(1, '/d?exp=1&sig=b')])
    const b = chunkSignature([c(1, '/d?exp=9&sig=y'), c(0, '/d?exp=9&sig=x')])
    expect(a).toBe(b)
  })

  it('changes when a chunk is added', () => {
    expect(chunkSignature([c(0, 'u')])).not.toBe(chunkSignature([c(0, 'u'), c(1, 'u')]))
  })

  it('distinguishes chunks without a URL', () => {
    expect(chunkSignature([c(0, '')])).not.toBe(chunkSignature([c(0, 'u')]))
  })
})

describe('displayScreenName', () => {
  it('strips generic parameters from controller class names', () => {
    expect(displayScreenName('StyleContextSplitViewNavigationController<NoStyleContext>')).toBe(
      'StyleContextSplitViewNavigationController',
    )
  })

  it('labels SwiftUI hosting controllers', () => {
    expect(displayScreenName('UIHostingController<ModifiedContent<AnyView, RootModifier>>')).toBe('SwiftUI view')
  })

  it('keeps plain names', () => {
    expect(displayScreenName('CheckoutScreen')).toBe('CheckoutScreen')
  })
})

describe('hasCapturedContent', () => {
  const n = (cls: string, children: ViewNode[] = [], text?: string): ViewNode => ({
    cls,
    frame: [0, 0, 402, 874],
    children,
    ...(text ? { text } : {}),
  })

  it('is false for a SwiftUI hosting tree with only container views', () => {
    const tree = n('UIWindow', [n('_UIHostingView<ModifiedContent<AnyView, RootModifier>>', [n('PlatformContainer')])])
    expect(hasCapturedContent(tree)).toBe(false)
  })

  it('is true when any node carries text', () => {
    expect(hasCapturedContent(n('UIView', [n('UILabel', [], 'Requests')]))).toBe(true)
  })

  it('is true for UIKit leaf controls without text', () => {
    expect(hasCapturedContent(n('UIView', [n('UIImageView')]))).toBe(true)
  })
})
