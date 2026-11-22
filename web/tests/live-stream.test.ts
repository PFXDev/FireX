import assert from 'node:assert/strict'
import { test } from 'node:test'
import { subscribeLiveStats } from '../src/lib/live-stream.ts'

test('live stream backs off only after permanent closure and stops on logout or unsubscribe', async (t) => {
  const sources: MockSource[] = []
  class MockSource extends EventTarget {
    static CLOSED = 2
    readyState = 0
    onerror?: () => Promise<void>
    constructor() {
      super()
      sources.push(this)
    }
    close() { this.readyState = MockSource.CLOSED }
  }
  const windowEvents = new EventTarget()
  for (const [key, value] of Object.entries({ EventSource: MockSource, window: windowEvents })) {
    const original = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { configurable: true, value })
    t.after(() => {
      if (original) Object.defineProperty(globalThis, key, original)
      else Reflect.deleteProperty(globalThis, key)
    })
  }
  let status = 200
  let signedOut = false
  windowEvents.addEventListener('firex:unauthorized', () => { signedOut = true })
  t.mock.method(globalThis, 'fetch', async () => new Response(null, { status }))
  t.mock.timers.enable({ apis: ['setTimeout'] })

  let snapshots = 0
  const stop = subscribeLiveStats(() => { snapshots++ }, () => {})
  const initial = sources[0]
  await initial.onerror!()
  t.mock.timers.tick(30000)
  assert.equal(sources.length, 1)
  initial.close()
  await initial.onerror!()
  t.mock.timers.tick(999)
  assert.equal(sources.length, 1)
  t.mock.timers.tick(1)
  assert.equal(sources.length, 2)
  sources[1].close()
  await sources[1].onerror!()
  t.mock.timers.tick(1999)
  assert.equal(sources.length, 2)
  t.mock.timers.tick(1)
  sources[2].dispatchEvent(new MessageEvent('snapshot', { data: '{}' }))
  assert.equal(snapshots, 1)
  sources[2].close()
  await sources[2].onerror!()
  t.mock.timers.tick(1000)
  assert.equal(sources.length, 4)

  status = 401
  sources[3].close()
  await sources[3].onerror!()
  t.mock.timers.tick(30000)
  assert.equal(signedOut, true)
  assert.equal(sources.length, 4)
  stop()

  status = 200
  const unsubscribe = subscribeLiveStats(() => {}, () => {})
  sources[4].close()
  const checking = sources[4].onerror!()
  unsubscribe()
  await checking
  t.mock.timers.tick(30000)
  assert.equal(sources.length, 5)
})
