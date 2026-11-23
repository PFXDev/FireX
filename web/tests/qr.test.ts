import assert from 'node:assert/strict'
import { test } from 'node:test'

import { encodeQrSvgData } from '../src/lib/qr.ts'

test('encodeQrSvgData generates valid SVG path geometry for ASCII URLs', () => {
  const result = encodeQrSvgData('https://sub.example.com/sub/abcdef123456')
  assert.ok(result.size >= 21, `expected matrix size >= 21, got ${result.size}`)
  assert.ok(result.path.startsWith('M'), 'expected SVG path commands')
  assert.ok(result.path.includes('h1v1h-1z'), 'expected unit square path rectangles')
})

test('encodeQrSvgData supports UTF-8 Chinese strings', () => {
  const result = encodeQrSvgData('https://sub.example.com/订阅?remark=香港专线')
  assert.ok(result.size >= 25)
  assert.ok(result.path.length > 100)
})

test('encodeQrSvgData handles long subscription URLs with base64 query', () => {
  const longToken = 'a'.repeat(120)
  const result = encodeQrSvgData(
    `https://very-long-subscription-domain.example.com/api/v1/client/subscribe?token=${longToken}&target=base64&flag=clash`,
  )
  assert.ok(result.size >= 41)
  assert.ok(result.path.length > 500)
})
