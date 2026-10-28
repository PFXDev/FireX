import assert from 'node:assert/strict'
import { test } from 'node:test'
import { NAV_GROUPS, isRouteWithin, parseRoute } from '../src/lib/navigation.ts'

test('hash routes ignore query strings and fall back to the overview', () => {
  assert.equal(parseRoute('#/inbound-configs?id=3'), 'inbound-configs')
  assert.equal(parseRoute('#/users'), 'users')
  assert.equal(parseRoute(''), 'overview')
  assert.equal(parseRoute('#/'), 'overview')
  assert.equal(parseRoute('#/missing'), 'overview')
})

test('a parent menu contains its sub-pages', () => {
  const inbounds = NAV_GROUPS.flatMap((group) => group.items).find((item) => item.route === 'inbounds')!
  assert.ok(isRouteWithin(inbounds, 'inbound-configs'))
  assert.ok(!isRouteWithin(inbounds, 'panels'))
})
