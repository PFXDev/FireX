import type { Inbound } from '@/api'

type Named = Pick<Inbound, 'name' | 'remoteRemark' | 'inboundTag'> & { emoji?: string }

/** The name an operator gave the inbound, falling back to what the panel calls it. */
export function inboundLabel(inbound: Named): string {
  return inbound.name || inbound.remoteRemark || inbound.inboundTag
}

/** inboundLabel with the inbound's emoji in front, for lists and titles. */
export function inboundTitle(inbound: Named): string {
  return inbound.emoji ? `${inbound.emoji} ${inboundLabel(inbound)}` : inboundLabel(inbound)
}

/** Reads `?id=` from the hash route, e.g. `#/inbound-configs?id=3`. */
export function hashID(): number | null {
  return Number(new URLSearchParams(window.location.hash.split('?')[1]).get('id')) || null
}
