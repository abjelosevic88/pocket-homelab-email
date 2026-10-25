// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds four months of practice):
//
//   DATA_DIR=/tmp/still-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Maya PORT=18306 ./pocket-mail
//   CDP=http://127.0.0.1:19271 BASE=http://127.0.0.1:18306 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18306'
const shots = process.env.SHOTS !== '0'
const dir = new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1500, still = true) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  if (still) await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
async function breathe(page, technique) {
  await go(page, `/session?mode=breath&technique=${technique}`, 800, false)
  await page.getByRole('button', { name: /begin/i }).first().click()
  await page.waitForTimeout(6200) // the get-ready countdown, then into the first breath
}

// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).toLowerCase().includes('streak'), 'Today shows the streak')
  await shot(page, 'desktop-home')
  await go(page, '/breathe')
  await shot(page, 'desktop-breathe')
  await go(page, '/meditate')
  await shot(page, 'desktop-meditate')
  await go(page, '/path/path')
  await shot(page, 'desktop-path')
  await go(page, '/stats', 2500)
  await shot(page, 'desktop-stats')
  await go(page, '/milestones')
  await shot(page, 'desktop-milestones')
  for (const p of ['/journal', '/path', '/path/path/settle-1', '/breathe/box', '/settings/practice', '/settings/apps', '/settings/reminders']) await go(page, p, 600)
  await breathe(page, 'coherent')
  await shot(page, 'desktop-session')
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  await breathe(page, 'box')
  await shot(page, 'phone-session')
  await ctx.close()
}

await browser.close()
if (errors.length) {
  console.error('browser errors:\n' + errors.join('\n'))
  process.exit(1)
}
console.log('ok')
