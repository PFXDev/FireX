import assert from 'node:assert/strict'
import { test } from 'node:test'
import { NAV_GROUPS, isRouteWithin, parseRoute, routeTrail } from '../src/lib/navigation.ts'

test('hash routes ignore query strings and fall back to the overview', () => {
  assert.equal(parseRoute('#/inbound-configs?id=3'), 'inbound-configs')
  assert.equal(parseRoute('#/users'), 'users')
  assert.equal(parseRoute(''), 'overview')
  assert.equal(parseRoute('#/'), 'overview')
  assert.equal(parseRoute('#/missing'), 'overview')
})

test('sub-pages expose their parent menu in the breadcrumb trail', () => {
  assert.deepEqual(routeTrail('inbound-templates'), [
    { title: '资源管理' },
    { title: '入站', route: 'inbounds' },
    { title: '入站模板' },
  ])
  assert.deepEqual(routeTrail('routing'), [{ title: '运维配置' }, { title: '分流' }])
})

test('a parent menu contains its sub-pages', () => {
  const inbounds = NAV_GROUPS.flatMap((group) => group.items).find((item) => item.route === 'inbounds')!
  assert.ok(isRouteWithin(inbounds, 'inbound-configs'))
  assert.ok(!isRouteWithin(inbounds, 'panels'))
})
