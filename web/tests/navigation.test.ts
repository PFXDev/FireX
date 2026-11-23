import assert from 'node:assert/strict'
import { test } from 'node:test'
import { NAV_GROUPS, isRouteWithin, parseHashLocation, parseRoute, routeTitle } from '../src/lib/navigation.ts'

test('hash routes ignore query strings and fall back to the overview', () => {
  assert.equal(parseRoute('#/inbound-configs?id=3'), 'inbound-configs')
  assert.equal(parseRoute('#/users'), 'users')
  assert.equal(parseRoute(''), 'overview')
  assert.equal(parseRoute('#/'), 'overview')
  assert.equal(parseRoute('#/missing'), 'overview')
})

test('parseHashLocation extracts route, query params, and fallback hash', () => {
  const usersLoc = parseHashLocation('#/users?status=failed&page=2')
  assert.equal(usersLoc.route, 'users')
  assert.equal(usersLoc.params.get('status'), 'failed')
  assert.equal(usersLoc.params.get('page'), '2')
  assert.equal(usersLoc.hash, '#/users?status=failed&page=2')

  const settingsLoc = parseHashLocation('#/settings?tab=security')
  assert.equal(settingsLoc.route, 'settings')
  assert.equal(settingsLoc.params.get('tab'), 'security')

  const fallbackLoc = parseHashLocation('')
  assert.equal(fallbackLoc.route, 'overview')
  assert.equal(fallbackLoc.hash, '#/overview')
  assert.equal(fallbackLoc.params.toString(), '')
})

test('routeTitle returns human-readable titles for top-level and sub-pages', () => {
  assert.equal(routeTitle('overview'), '总览')
  assert.equal(routeTitle('users'), '用户')
  assert.equal(routeTitle('settings'), '设置')
  assert.equal(routeTitle('inbounds'), '入站列表')
  assert.equal(routeTitle('inbound-configs'), '入站配置')
  assert.equal(routeTitle('external-subscriptions'), '外部订阅')
})

test('a parent menu contains its sub-pages', () => {
  const items = NAV_GROUPS.flatMap((group) => group.items)
  const inbounds = items.find((item) => item.route === 'inbounds')!
  assert.ok(isRouteWithin(inbounds, 'inbound-configs'))
  assert.ok(!isRouteWithin(inbounds, 'panels'))
  const nodeGroups = items.find((item) => item.route === 'node-groups')!
  assert.ok(isRouteWithin(nodeGroups, 'external-subscriptions'))
  assert.ok(!isRouteWithin(nodeGroups, 'inbounds'))
})
