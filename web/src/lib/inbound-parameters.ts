export function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

export function concealed(value: unknown, key = ''): unknown {
  // VLESS decryption embeds the server's private key. The literal "none"
  // carries no credential and should remain readable in the inspector.
  if (key.toLowerCase() === 'decryption' && value === 'none') return value
  if (/private|password|secret|token|seed|^key$|^auth$|^decryption$/i.test(key) && value) return '••••••••'
  if (Array.isArray(value)) return value.map((item) => concealed(item))
  if (isObject(value)) return Object.fromEntries(Object.entries(value).map(([name, item]) => [name, concealed(item, name)]))
  return value
}
