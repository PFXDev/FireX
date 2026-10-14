import type { Egress, Policy, RoutingMatrix, RuleSet } from '../api'

/** A policy and its egresses, keyed by the column they belong to. */
export type RoutingRow = {
  policy: Policy
  cells: Map<number, Egress>
}

export const RULE_SET_TYPE = 'RULE-SET'

export function routingDocument(
  rows: RoutingRow[],
  ruleSets: RuleSet[],
): Pick<RoutingMatrix, 'policies' | 'egresses' | 'ruleSets'> {
  return {
    policies: rows.map((row) => row.policy),
    egresses: rows.flatMap((row, index) =>
      [...row.cells.entries()].map(([profileId, egress]) => ({
        ...egress,
        policyIndex: index,
        // An inherited egress still carries the default's profileId.
        // The map key identifies the column the operator actually edited.
        profileId,
      })),
    ),
    ruleSets,
  }
}

/** Repoints every RULE-SET rule that named `previous`; the server does the same on save. */
export function renameRuleSetReferences(rows: RoutingRow[], previous: string, next: string): RoutingRow[] {
  return rows.map((row) => {
    if (!row.policy.rules.some((rule) => isRuleSetRule(rule, previous))) return row
    return {
      ...row,
      policy: {
        ...row.policy,
        rules: row.policy.rules.map((rule) => (isRuleSetRule(rule, previous) ? { ...rule, value: next } : rule)),
      },
    }
  })
}

/** Drops the RULE-SET rules that named a deleted set, which would otherwise fail validation. */
export function dropRuleSetReferences(rows: RoutingRow[], name: string): RoutingRow[] {
  return rows.map((row) => {
    if (!row.policy.rules.some((rule) => isRuleSetRule(rule, name))) return row
    return {
      ...row,
      policy: { ...row.policy, rules: row.policy.rules.filter((rule) => !isRuleSetRule(rule, name)) },
    }
  })
}

/** Names of the policies with a RULE-SET rule pointing at the set. */
export function ruleSetUsers(rows: RoutingRow[], name: string): string[] {
  return rows.filter((row) => row.policy.rules.some((rule) => isRuleSetRule(rule, name))).map((row) => row.policy.name)
}

/** Counts the rule lines a client will be served, mirroring the server's parser. */
export function ruleSetLineCount(payload: string): number {
  return payload.split('\n').filter((line) => {
    const trimmed = line.trim()
    return trimmed !== '' && !trimmed.startsWith('#')
  }).length
}

function isRuleSetRule(rule: Policy['rules'][number], name: string): boolean {
  return rule.type === RULE_SET_TYPE && rule.value.trim() === name
}
