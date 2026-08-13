import { chromium } from 'playwright-core'
import { mkdir } from 'node:fs/promises'

const baseURL = process.env.VELIN_TEST_URL || 'http://127.0.0.1:8377'
const password = process.env.VELIN_TEST_PASSWORD
if (!password) throw new Error('VELIN_TEST_PASSWORD is required')
await mkdir('../artifacts', { recursive: true })
const browser = await chromium.launch({ executablePath: '/usr/bin/chromium', headless: true, args: ['--no-sandbox'] })

async function verify(name, viewport) {
  const context = await browser.newContext({ viewport })
  const page = await context.newPage()
  const errors = []
  const httpErrors = []
  page.on('console', msg => {
    if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource: the server responded with a status of 401')) errors.push(msg.text())
  })
  page.on('pageerror', err => errors.push(err.message))
  page.on('response', response => {
    if (response.status() >= 400 && !(response.status() === 401 && response.url().endsWith('/api/auth/me'))) httpErrors.push(`${response.status()} ${response.url()}`)
  })
  await page.goto(`${baseURL}/login`, { waitUntil: 'networkidle' })
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL('**/workspace')
  await page.getByText('没有打开的终端').waitFor()
  const metrics = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth, title: document.title }))
  if (metrics.scrollWidth > metrics.width) throw new Error(`${name}: horizontal overflow ${metrics.scrollWidth} > ${metrics.width}`)
  if (metrics.title !== 'Velin Web SSH') throw new Error(`${name}: unexpected title ${metrics.title}`)
  if (errors.length) throw new Error(`${name}: console errors: ${errors.join(' | ')}`)
  if (httpErrors.length) throw new Error(`${name}: HTTP errors: ${httpErrors.join(' | ')}`)
  await page.screenshot({ path: `../artifacts/${name}.png`, fullPage: true })
  console.log(JSON.stringify({ name, ...metrics, errors: errors.length }))
  await context.close()
}

try {
  await verify('desktop', { width: 1440, height: 900 })
  await verify('mobile', { width: 390, height: 844 })
} finally {
  await browser.close()
}
