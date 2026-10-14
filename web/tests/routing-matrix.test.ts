import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Egress, Policy } from '../src/api.ts'
import {
  renameRuleSetReferences,
  routingDocument,
  ruleSetLineCount,
  ruleSetUsers,
} from '../src/lib/routing-matrix.ts'

test('editing an inherited egress saves it to its profile column without duplicating the default', () => {
  const policy: Policy = {
    id: 52, name: 'OpenAI', icon: '', isFinal: false,
    enabled: true, remark: '', rules: [],
  }
  const defaultEgress: Egress = {
    policyIndex: 3, profileId: 0, type: 'select', testUrl: '',
    interval: 300, tolerance: 50, hidden: false, clientHidden: false,
    members: [{ kind: 'builtin', ref: 'DIRECT' }],
  }
  // The dialog starts with the inherited default, including its old identity.
  const override = { ...defaultEgress, clientHidden: true }
  const hidden = { ...defaultEgress, hidden: true }
  const cells = new Map([[0, defaultEgress], [7, override], [9, hidden]])
  const doc = routingDocument([{ policy, cells }], [])

  assert.deepEqual(doc.egresses, [
    { ...defaultEgress, policyIndex: 0, profileId: 0 },
    { ...override, policyIndex: 0, profileId: 7 },
    { ...hidden, policyIndex: 0, profileId: 9 },
  ])
  assert.equal(override.profileId, 0, 'serialization must not mutate the editor draft')

  // Restoring inheritance removes only that override, leaving the default intact.
  cells.delete(7)
  assert.deepEqual(routingDocument([{ policy, cells }], []).egresses.map((cell) => cell.profileId), [0, 9])
})

test('reordering policies saves every cell under its current row index', () => {
  const rows = [52, 53].map((id, policyIndex) => ({
    policy: {
      id, name: `Policy ${id}`, icon: '', isFinal: false,
      enabled: true, remark: '', rules: [],
    },
    cells: new Map([[0, {
      policyIndex, profileId: 0, type: 'select', testUrl: '',
      interval: 300, tolerance: 50, hidden: false, clientHidden: false,
      members: [{ kind: 'builtin' as const, ref: 'DIRECT' }],
    }]]),
  }))
  const doc = routingDocument(rows.reverse(), [])
  assert.deepEqual(doc.policies.map((policy) => policy.id), [53, 52])
  assert.deepEqual(doc.egresses.map((cell) => cell.policyIndex), [0, 1])
})

test('renaming a rule set repoints only the RULE-SET rules that named it', () => {
  const rule = (type: string, value: string): Policy['rules'][number] => ({ type, value, noResolve: false, disabled: false })
  const rows = [
    { name: 'Downloads', rules: [rule('RULE-SET', 'bulk'), rule('GEOSITE', 'bulk')] },
    { name: 'Other', rules: [rule('RULE-SET', 'other')] },
  ].map(({ name, rules }, id) => ({
    policy: { id, name, icon: '', isFinal: false, enabled: true, remark: '', rules },
    cells: new Map<number, Egress>(),
  }))

  const renamed = renameRuleSetReferences(rows, 'bulk', 'downloads')
  assert.deepEqual(renamed[0].policy.rules.map((r) => r.value), ['downloads', 'bulk'])
  assert.equal(renamed[1], rows[1], 'untouched rows keep their identity')
  assert.deepEqual(ruleSetUsers(renamed, 'downloads'), ['Downloads'])
  assert.deepEqual(ruleSetUsers(renamed, 'bulk'), [])
})

test('rule set line count skips blank lines and comments', () => {
  assert.equal(ruleSetLineCount('# hosts\n\nGEOSITE,huggingface\n  DOMAIN,a.com  \n'), 2)
})
