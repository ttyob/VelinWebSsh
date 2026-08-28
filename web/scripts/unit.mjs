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

const { buildGitChangeTree, isStaged, parseGitBranches, parseGitCommits, parseGitRemotes, parseGitStatus, parseUnifiedDiff } = await loadModule(new URL('../src/git.ts', import.meta.url))
const gitStatus = parseGitStatus('## feature/test...origin/feature/test [ahead 2, behind 1]\0 M file with space.txt\0?? src/new file.txt\0R  src/new-name.ts\0src/old-name.ts\0')
assert.equal(gitStatus.branch, 'feature/test')
assert.equal(gitStatus.tracking, 'origin/feature/test')
assert.equal(gitStatus.ahead, 2)
assert.equal(gitStatus.behind, 1)
assert.deepEqual(gitStatus.changes.map((item) => item.path), ['file with space.txt', 'src/new file.txt', 'src/new-name.ts'])
assert.equal(gitStatus.changes[2].originalPath, 'src/old-name.ts')
assert.equal(isStaged(gitStatus.changes[0]), false)
assert.equal(isStaged(gitStatus.changes[1]), false)
assert.equal(isStaged({ index: 'M', worktree: ' ', path: 'staged.txt' }), true)
assert.equal(parseGitStatus('## HEAD (no branch)\0').branch, 'HEAD')
const gitTree = buildGitChangeTree(gitStatus.changes)
assert.deepEqual(gitTree.map((item) => item.label), ['src', 'file with space.txt'])
assert.deepEqual(gitTree[0].children.map((item) => item.label), ['new file.txt', 'new-name.ts'])
const splitDiff = parseUnifiedDiff('diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1,3 +1,4 @@\n same\n-old\n+new\n+added\n tail\n')
assert.deepEqual(splitDiff, [
  { leftNumber: 1, rightNumber: 1, left: 'same', right: 'same', kind: 'context' },
  { leftNumber: 2, rightNumber: 2, left: 'old', right: 'new', kind: 'change' },
  { leftNumber: undefined, rightNumber: 3, left: '', right: 'added', kind: 'add' },
  { leftNumber: 3, rightNumber: 4, left: 'tail', right: 'tail', kind: 'context' },
])

const gitBranches = parseGitBranches('*\trefs/heads/feature/test\tabc1234\torigin/feature/test\t[ahead 2]\tlocal subject\n \trefs/remotes/origin/feature/test\tabc1234\t\t\tremote subject\n \trefs/remotes/origin/HEAD\tabc1234\t\t\tdefault\n')
assert.equal(gitBranches.length, 2)
assert.deepEqual(gitBranches[0], { current: true, name: 'feature/test', hash: 'abc1234', subject: 'local subject', remote: false, upstream: 'origin/feature/test', tracking: 'ahead 2' })
assert.equal(gitBranches[1].name, 'origin/feature/test')
assert.equal(gitBranches[1].remote, true)
assert.deepEqual(parseGitBranches('*%x09refs/heads/main%x09abc%x09subject'), [])

assert.equal(parseGitCommits('abcdef\tabcdef\tA User\t2026-08-28 10:00:00 +0800\tsubject\twith tab')[0].subject, 'subject\twith tab')
assert.deepEqual(parseGitRemotes('origin\tgit@example.com:team/repo.git (fetch)\norigin\tgit@example.com:team/repo.git (push)\n'), [{ name: 'origin', url: 'git@example.com:team/repo.git' }])

const { deriveCounterRates, parseMonitorCounters, parseSSHMonitor } = await loadModule(new URL('../src/hostMonitor.ts', import.meta.url))
const beforeCounters = parseMonitorCounters('cores\t4\ncpu\t1000\t400\nnetwork\t10000\t20000\n', 1000)
const nextCounters = parseMonitorCounters('cores\t4\ncpu\t1200\t450\nnetwork\t13000\t24000\n', 3000)
assert.deepEqual(deriveCounterRates(beforeCounters, nextCounters), { cpuPercent: 75, receivedPerSecond: 1500, sentPerSecond: 2000 })
const sshMonitor = parseSSHMonitor('__SOURCE__\tjournalctl\n1756350000.000 host sshd[1]: Accepted publickey for root from 10.0.0.1 port 22 ssh2\n1756350060.000 host sshd[2]: Failed password for invalid user test from 10.0.0.2 port 23 ssh2\n__ACTIVE__\nroot pts/0 2026-08-28 10:00 (10.0.0.1)\n')
assert.equal(sshMonitor.available, true)
assert.equal(sshMonitor.successful, 1)
assert.equal(sshMonitor.failed, 1)
assert.equal(sshMonitor.activeSessions, 1)
assert.deepEqual(sshMonitor.records.map((item) => [item.status, item.user, item.address]), [['failed', 'test', '10.0.0.2'], ['success', 'root', '10.0.0.1']])

console.log('frontend unit checks passed')
