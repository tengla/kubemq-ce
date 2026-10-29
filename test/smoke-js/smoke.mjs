// Smoke test: the kubemq-js SDK against a running kubemq-ce server.
//
//   node smoke.mjs [host:port]        (default: $KUBEMQ_ADDRESS or localhost:50000)
//
// Set KUBEMQ_TOKEN when the server has authentication enabled. Each run uses
// fresh channels named smoke.<run id>.* on the target server.
//
// A check can be marked expectFail for SDK features the server doesn't
// support. Those are reported as XFAIL and don't fail the run; if one starts
// passing it shows up as XPASS.
import {
  KubeMQClient,
  createEventMessage,
  createEventStoreMessage,
  createQueueMessage,
  createCommand,
  createQuery,
  EventStoreStartPosition,
} from 'kubemq-js'

const address = process.argv[2] ?? process.env.KUBEMQ_ADDRESS ?? 'localhost:50000'
const run = Date.now().toString(36)
const ch = (name) => `smoke.${run}.${name}`
const text = (bytes) => new TextDecoder().decode(bytes)
const results = []

// A stream that the server rejects (for example without a token) errors
// outside any awaited call; report it instead of dumping a stack trace.
process.on('uncaughtException', (err) => {
  console.error(`error: ${err?.constructor?.name ?? 'Error'}: ${err?.message ?? err}`)
  process.exit(1)
})

async function withTimeout(promise, ms, what) {
  let timer
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`timed out after ${ms}ms waiting for ${what}`)), ms)
  })
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer))
}

async function check(name, fn, { expectFail = false } = {}) {
  try {
    const detail = await withTimeout(fn(), 10000, name)
    results.push({ name, ok: true, expectFail, detail })
  } catch (err) {
    results.push({ name, ok: false, expectFail, detail: `${err?.constructor?.name ?? 'Error'}: ${err?.message ?? err}` })
  }
}

// Resolves with the first message a subscription delivers.
function firstMessage(subscribe) {
  let resolve, reject
  const done = new Promise((res, rej) => ((resolve = res), (reject = rej)))
  const sub = subscribe(resolve, reject)
  return { done, sub }
}

const pause = (ms) => new Promise((r) => setTimeout(r, ms))

// Subscriptions report their own cancel() as an error; only show real ones.
const subscriptionError = (kind) => (err) => {
  if (!/cancelled on client/i.test(err?.message ?? '')) console.error(`${kind} subscription error:`, err?.message ?? err)
}

const client = await KubeMQClient.create({ address, clientId: `smoke-${run}`, credentials: process.env.KUBEMQ_TOKEN || undefined })

await check('ping', async () => {
  const info = await client.ping()
  return `host=${info.host} version=${info.version}`
})

await check('events: subscribe + send', async () => {
  const { done, sub } = firstMessage((onEvent, onError) => client.subscribeToEvents({ channel: ch('events'), onEvent, onError }))
  await pause(300)
  await client.sendEvent(createEventMessage({ channel: ch('events'), body: 'hello-event' }))
  const msg = await done
  sub.cancel?.()
  return `received "${text(msg.body)}"`
})

await check('events store: send + replay from first', async () => {
  await client.sendEventStore(createEventStoreMessage({ channel: ch('store'), body: 'hello-store' }))
  const { done, sub } = firstMessage((onEvent, onError) =>
    client.subscribeToEventsStore({ channel: ch('store'), startFrom: EventStoreStartPosition.StartFromFirst, onEvent, onError }),
  )
  const msg = await done
  sub.cancel?.()
  return `received "${text(msg.body)}" seq=${msg.sequence}`
})

await check('commands: respond + send', async () => {
  const sub = client.subscribeToCommands({
    channel: ch('cmd'),
    onCommand: (cmd) => client.sendCommandResponse({ requestId: cmd.id, id: cmd.id, replyChannel: cmd.replyChannel, isExecuted: true, executed: true }),
    onError: subscriptionError('command'),
  })
  await pause(300)
  const res = await client.sendCommand(createCommand({ channel: ch('cmd'), body: 'do-it', timeoutInSeconds: 5 }))
  sub.cancel?.()
  if (!(res.executed ?? res.isExecuted)) throw new Error(`not executed: ${res.error || 'no error given'}`)
  return 'executed'
})

await check('queries: respond + send', async () => {
  const sub = client.subscribeToQueries({
    channel: ch('qry'),
    onQuery: (q) => client.sendQueryResponse({ id: q.id, replyChannel: q.replyChannel, executed: true, body: new TextEncoder().encode(`answer-to-${text(q.body)}`) }),
    onError: subscriptionError('query'),
  })
  await pause(300)
  const res = await client.sendQuery(createQuery({ channel: ch('qry'), body: 'q1', timeoutInSeconds: 5 }))
  sub.cancel?.()
  const body = res.body ? text(res.body) : ''
  if (body !== 'answer-to-q1') throw new Error(`unexpected reply "${body}"`)
  return `reply "${body}"`
})

await check('queues: send one', async () => {
  const res = await client.sendQueueMessage(createQueueMessage({ channel: ch('q'), body: 'job-1' }))
  if (res.isError) throw new Error(res.error)
  return `id=${res.messageId ?? res.id}`
})

await check('queues: send batch of 3', async () => {
  const res = await client.sendQueueMessagesBatch([2, 3, 4].map((n) => createQueueMessage({ channel: ch('q'), body: `job-${n}` })))
  const sent = res.results ?? []
  if (sent.length !== 3) throw new Error(`expected 3 results, got ${JSON.stringify(res).slice(0, 160)}`)
  return `${sent.length} sent`
})

await check('queues: peek', async () => {
  const msgs = await client.peekQueueMessages({ channel: ch('q'), waitTimeoutSeconds: 2, maxMessages: 10 })
  if (msgs.length !== 4) throw new Error(`expected 4 waiting, got ${msgs.length}`)
  return `${msgs.length} waiting: ${msgs.map((m) => text(m.body)).join(', ')}`
})

await check('queues: receive + ack one', async () => {
  const msgs = await client.receiveQueueMessages({ channel: ch('q'), waitTimeoutSeconds: 2, maxMessages: 1 })
  if (msgs.length !== 1) throw new Error(`expected 1 message, got ${msgs.length}`)
  for (const m of msgs) await m.ack()
  return `received ${text(msgs[0].body)} and acked`
})

await check('queues: stream + ackAll', async () => {
  const handle = client.streamQueueMessages({ channel: ch('q'), waitTimeoutSeconds: 2, maxMessages: 1 })
  const msgs = await new Promise((resolve, reject) => {
    handle.onMessages(resolve)
    handle.onError(reject)
  })
  handle.ackAll()
  await pause(300)
  handle.close()
  return `streamed ${msgs.map((m) => text(m.body)).join(', ')} and acked`
})

await check('queues: consumeQueue (async iterator)', async () => {
  for await (const batch of client.consumeQueue({ channel: ch('q'), waitTimeoutSeconds: 2, maxMessages: 1 })) {
    const bodies = batch.messages.map((m) => text(m.body))
    await batch.ackAll?.()
    return `consumed ${bodies.join(', ')}`
  }
  throw new Error('iterator ended without a batch')
})

await check('queues: ackAll remaining', async () => {
  const n = await client.ackAllQueueMessages(ch('q'), 2)
  if (n !== 1) throw new Error(`expected to ack 1, acked ${n}`)
  return `acked ${n}`
})

await check('queues: purge', async () => {
  await client.sendQueueMessage(createQueueMessage({ channel: ch('q'), body: 'to-purge' }))
  await client.purgeQueue(ch('q'))
  const left = await client.peekQueueMessages({ channel: ch('q'), waitTimeoutSeconds: 1 })
  if (left.length !== 0) throw new Error(`${left.length} left after purge`)
  return 'queue empty'
})

// The SDK ignores the server's answer to create and delete, so check the
// effect with a list.
const listedQueues = async () => (await client.listQueuesChannels(`smoke.${run}`)).map((c) => c.name)

await check('channels: create queues channel', async () => {
  await client.createQueuesChannel(ch('created'))
  if (!(await listedQueues()).includes(ch('created'))) throw new Error('not listed after create')
  return 'ok'
})

await check('channels: list queues channels', async () => {
  const list = await client.listQueuesChannels(`smoke.${run}`)
  return `${list.length} channels: ${list.map((c) => c.name).join(', ')}`
})

await check('channels: delete queues channel', async () => {
  await client.deleteQueuesChannel(ch('created'))
  if ((await listedQueues()).includes(ch('created'))) throw new Error('still listed after delete')
  return 'ok'
})

await client.close().catch(() => { })

const label = (r) => (r.ok ? (r.expectFail ? 'XPASS' : 'PASS ') : r.expectFail ? 'XFAIL' : 'FAIL ')
for (const r of results) console.log(`${label(r)}  ${r.name}  ->  ${r.detail}`)
const failed = results.filter((r) => !r.ok && !r.expectFail).length
const passed = results.filter((r) => r.ok && !r.expectFail).length
const expected = results.filter((r) => !r.expectFail).length
console.log(`\n${passed}/${expected} passed against ${address}` + (failed ? `, ${failed} failed` : ''))
process.exit(failed ? 1 : 0)
