export type EventDisplayInfo = {
  label: string
  category: string
}

export const KNOWN_EVENTS: Record<string, EventDisplayInfo> = {
  landing_view: {
    label: 'Landing Page View',
    category: 'Acquisition',
  },
  view_product: {
    label: 'Product Detail View',
    category: 'Engagement',
  },
  add_to_cart: {
    label: 'Add to Cart',
    category: 'Intent',
  },
  checkout_start: {
    label: 'Begin Checkout',
    category: 'Checkout',
  },
  purchase_completed: {
    label: 'Purchase Completed',
    category: 'Conversion',
  },
}

export function formatEventName(raw: string): string {
  if (KNOWN_EVENTS[raw]) {
    return KNOWN_EVENTS[raw].label
  }
  return raw
    .replace(/[_-]+/g, ' ')
    .trim()
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

export type FunnelPresetTemplate = {
  id: string
  name: string
  description: string
  steps: string[]
}

export const POPULAR_TEMPLATES: FunnelPresetTemplate[] = [
  {
    id: 'ecommerce',
    name: 'E-Commerce Checkout',
    description: 'Landing to Product View to Add to Cart to Checkout to Purchase',
    steps: ['landing_view', 'view_product', 'add_to_cart', 'checkout_start', 'purchase_completed'],
  },
  {
    id: 'product-intent',
    name: 'Product Engagement',
    description: 'Landing to Product View to Add to Cart',
    steps: ['landing_view', 'view_product', 'add_to_cart'],
  },
  {
    id: 'checkout-completion',
    name: 'Cart to Purchase',
    description: 'Add to Cart to Begin Checkout to Purchase Completed',
    steps: ['add_to_cart', 'checkout_start', 'purchase_completed'],
  },
]
