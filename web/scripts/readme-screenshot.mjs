import { chromium } from 'playwright-core'

const baseURL = process.env.VELIN_SCREENSHOT_URL || 'http://127.0.0.1:4173'
const output = '../.github/assets/velin-workspace.png'

const hosts = [
  host('api-prod', 'API Gateway', '10.20.1.12', '生产环境/应用服务', 1, 24),
  host('worker-prod', 'Worker Node', '10.20.1.18', '生产环境/应用服务', 2, 38),
  host('postgres-prod', 'PostgreSQL', '10.20.2.15', '生产环境/数据库', 1, 31),
  host('worker-staging', 'Staging Worker', '10.30.1.21', '预发布/集群节点', 1, 52),
]

const sessions = [
  session('session-api', 'api-prod', 'API Gateway'),
  session('session-worker', 'worker-staging', 'Staging Worker'),
]

const layout = {
  tabs: ['workspace-main'],
  active: 'workspace-main',
  trees: {
    'workspace-main': {
      type: 'split',
      id: 'split-main',
      direction: 'horizontal',
      ratio: 0.52,
      first: { type: 'leaf', id: 'pane-api', sessionID: 'session-api' },
      second: { type: 'leaf', id: 'pane-worker', sessionID: 'session-worker' },
    },
  },
  focused: { 'workspace-main': 'pane-api' },
  maximized: {},
}

const terminalOutput = {
  'session-api': [
    '\u001b[1;32mops@api-prod\u001b[0m:\u001b[1;34m~\u001b[0m$ uptime',
    ' 05:52:18 up 128 days,  3:41,  2 users,  load average: 0.18, 0.22, 0.20',
    '\u001b[1;32mops@api-prod\u001b[0m:\u001b[1;34m~\u001b[0m$ docker ps',
    'NAME              STATUS                   PORT',
    'gateway           Up 18 days (healthy)     443/tcp',
    'auth-service      Up 18 days (healthy)     9000/tcp',
    'metrics-agent     Up 42 days               9100/tcp',
    '',
    '\u001b[1;32mops@api-prod\u001b[0m:\u001b[1;34m~\u001b[0m$ ',
  ].join('\r\n'),
  'session-worker': [
    '\u001b[1;32mdeploy@staging\u001b[0m:\u001b[1;34m~\u001b[0m$ kubectl get pods',
    'NAME                 READY   STATUS    AGE',
    'api-7f8c76497f       2/2     Running   4d',
    'worker-6d9b8b87d8    1/1     Running   4d',
    'scheduler-5f7c9d76   1/1     Running   11d',
    'redis-0              1/1     Running   31d',
    '',
    '\u001b[1;32mdeploy@staging\u001b[0m:\u001b[1;34m~\u001b[0m$ ',
  ].join('\r\n'),
}

const browser = await chromium.launch({
  executablePath: '/usr/bin/chromium',
  headless: true,
  args: ['--no-sandbox'],
})

try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
    colorScheme: 'dark',
  })
  const page = await context.newPage()
  const errors = []

  page.on('console', message => {
    if (message.type() === 'error') errors.push(message.text())
  })
  page.on('pageerror', error => errors.push(error.message))

  await page.addInitScript(({ outputs }) => {
    class DemoWebSocket {
      static CONNECTING = 0
      static OPEN = 1
      static CLOSING = 2
      static CLOSED = 3

      constructor(url) {
        this.url = String(url)
        this.readyState = DemoWebSocket.CONNECTING
        this.bufferedAmount = 0
        this.extensions = ''
        this.protocol = ''
        this.binaryType = 'blob'
        const sessionID = this.url.match(/\/ws\/sessions\/([^?]+)/)?.[1] || ''
        window.setTimeout(() => {
          this.readyState = DemoWebSocket.OPEN
          this.onopen?.(new Event('open'))
          const data = btoa(outputs[sessionID] || '')
          this.onmessage?.(new MessageEvent('message', {
            data: JSON.stringify({
              type: 'hello',
              clientID: 'readme-demo',
              controller: 'readme-demo',
              status: 'attached',
              streamID: `stream-${sessionID}`,
              offset: data.length,
              historySize: 680,
              data,
            }),
          }))
        }, 80)
      }

      send(value) {
        try {
          const message = JSON.parse(String(value))
          if (message.type === 'ping') {
            window.setTimeout(() => {
              this.onmessage?.(new MessageEvent('message', {
                data: JSON.stringify({ type: 'pong' }),
              }))
            }, 18)
          }
        } catch {}
      }

      close() {
        if (this.readyState === DemoWebSocket.CLOSED) return
        this.readyState = DemoWebSocket.CLOSED
        this.onclose?.(new CloseEvent('close', { code: 1000, wasClean: true }))
      }

      addEventListener(type, listener) {
        this[`on${type}`] = listener
      }

      removeEventListener(type, listener) {
        if (this[`on${type}`] === listener) this[`on${type}`] = null
      }
    }
    window.WebSocket = DemoWebSocket
    localStorage.setItem('velin-interface-theme', 'dark')
    localStorage.setItem('velin-accent-color', '#60a879')
    localStorage.setItem('velin-sidebar-width', '286')
  }, { outputs: terminalOutput })

  await page.route('**/api/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = {}
    if (path === '/api/auth/me') body = { id: 'admin', username: 'admin', role: 'admin', disabled: false }
    else if (path === '/api/hosts') body = hosts
    else if (path === '/api/credentials') body = []
    else if (path === '/api/sessions') body = sessions
    else if (path === '/api/workspace' && request.method() === 'GET') body = { layout, version: 1 }
    else if (path === '/api/workspace') body = { version: 2 }
    else if (path === '/api/preferences') body = {
      theme: 'dark', accentColor: '#60a879', terminalTheme: 'velin', fontSize: 14,
      lineHeight: 1.25, fontWeight: 400, letterSpacing: 0, cursorStyle: 'block',
      cursorBlink: true, pasteGuard: true, visualBell: true, soundBell: false,
      lockEnabled: false, autoLockMinutes: 15, lockOnShortcut: true,
    }
    else if (path === '/api/web-services') body = [
      { id: 'grafana', hostID: 'api-prod', name: 'Grafana', proxyMode: 'path', listenPort: 0, targetURL: 'http://127.0.0.1:3000', upstreamHost: '', skipTLSVerify: false },
      { id: 'admin', hostID: 'postgres-prod', name: 'DB Admin', proxyMode: 'path', listenPort: 0, targetURL: 'http://127.0.0.1:8080', upstreamHost: '', skipTLSVerify: false },
    ]
    else if (path === '/api/auth/lock-pin') body = { configured: false }
    else if (path === '/api/csrf') body = { token: 'readme-screenshot-token-000000000000000000000000' }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })

  await page.goto(`${baseURL}/workspace`, { waitUntil: 'networkidle' })
  await page.locator('.terminal-pane').first().waitFor()
  await page.waitForTimeout(900)
  await page.screenshot({ path: output, fullPage: false })
  if (errors.length) throw new Error(`browser errors: ${errors.join(' | ')}`)
  await context.close()
} finally {
  await browser.close()
}

function host(id, name, address, groupName, sortOrder, latency) {
  return {
    id, name, address, port: 22, username: id.includes('staging') ? 'deploy' : 'ops',
    credentialID: '', hasPassword: true, groupName, sortOrder, tags: '', notes: '',
    initialDirectory: '~', connectTimeout: 10, keepaliveInterval: 30, maxRetries: 3,
    terminalType: 'xterm-256color', sessionMode: 'tmux', jumpHostID: '',
    platform: '', distribution: '', lastStatus: 'online', lastLatencyMs: latency,
  }
}

function session(id, hostID, name) {
  return {
    id, userID: 'admin', hostID, credentialID: '', name,
    remoteUser: hostID.includes('staging') ? 'deploy' : 'ops', sessionMode: 'tmux',
    tmuxSocket: '', tmuxName: '', ownerMarker: '', status: 'attached', lastError: '',
    createdAt: '2026-08-31T05:30:00Z', updatedAt: '2026-08-31T05:50:00Z',
  }
}
