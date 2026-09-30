import assert from 'node:assert/strict'
import { test } from 'node:test'
import { concealed } from '../src/lib/inbound-parameters.ts'

test('hidden parameters and copied JSON omit nested VLESS and transport secrets', () => {
  const parameters = {
    settings: { decryption: 'mlkem768x25519plus.native.0rtt.600s.vless-private-key' },
    streamSettings: {
      realitySettings: { privateKey: 'reality-private-key', mldsa65Seed: 'signing-seed', publicKey: 'public-key' },
      tlsSettings: { certificates: [{ key: ['pem-private-key'], certificate: ['public-cert'] }] },
    },
  }
  const visible = concealed(parameters)
  const copied = JSON.stringify(visible)
  for (const secret of ['vless-private-key', 'reality-private-key', 'signing-seed', 'pem-private-key']) {
    assert.ok(!copied.includes(secret), `${secret} leaked into hidden/copied parameters`)
  }
  assert.ok(copied.includes('public-key'))
  assert.ok(copied.includes('public-cert'))
  // Revealing later must still have the original values, without a refetch.
  assert.ok(JSON.stringify(parameters).includes('vless-private-key'))
})

test('non-secret decryption values remain readable', () => {
  for (const decryption of ['none', '', null]) {
    assert.deepEqual(concealed({ settings: { decryption } }), { settings: { decryption } })
  }
})
