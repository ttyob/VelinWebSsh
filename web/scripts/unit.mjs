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

const { tmuxInstallGuide } = await loadModule(new URL('../src/tmuxInstall.ts', import.meta.url))
assert.equal(tmuxInstallGuide('linux', 'debian').command, 'sudo apt-get update && sudo apt-get install -y tmux')
assert.equal(tmuxInstallGuide('linux', 'rocky').command, 'sudo dnf install -y tmux')
assert.equal(tmuxInstallGuide('linux', 'alpine').command, 'sudo apk add tmux')
assert.equal(tmuxInstallGuide('windows', '').supported, false)
assert.match(tmuxInstallGuide('linux', 'unknown').command, /command -v apt-get/)

const { resolveTabAttention, terminalOutputSettleDelay } = await loadModule(new URL('../src/terminalAttention.ts', import.meta.url))
assert.equal(terminalOutputSettleDelay, 5000)
assert.equal(resolveTabAttention([{ name: 'server', status: 'ended', notice: 'settled' }]).kind, 'ended')
assert.equal(resolveTabAttention([{ name: 'server', status: 'attached', notice: 'bell' }]).kind, 'bell')
assert.equal(resolveTabAttention([{ name: 'server', status: 'auth_required', notice: 'bell' }]).kind, 'required')
assert.equal(resolveTabAttention([{ name: 'server', status: 'attached', notice: 'settled' }]).kind, 'settled')
assert.equal(resolveTabAttention([{ name: 'one', status: 'ended' }, { name: 'two', status: 'attached', notice: 'settled' }]).count, 2)
assert.equal(resolveTabAttention([{ name: 'server', status: 'attached' }]), undefined)

console.log('frontend unit checks passed')
