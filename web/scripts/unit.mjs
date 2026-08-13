import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

async function loadModule(path) {
  const source = await readFile(path, 'utf8')
  const output = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2022 } }).outputText
  return import(`data:text/javascript;base64,${Buffer.from(output).toString('base64')}`)
}

const { reconnectDelay } = await loadModule(new URL('../src/reconnect.ts', import.meta.url))
assert.equal(reconnectDelay(0, 0.5), 1000)
assert.equal(reconnectDelay(1, 0.5), 2000)
assert.equal(reconnectDelay(5, 0.5), 30000)
assert.equal(reconnectDelay(20, 1), 36000)
assert.equal(reconnectDelay(0, 0), 800)

console.log('frontend unit checks passed')
