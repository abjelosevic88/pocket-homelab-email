// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds two demo accounts):
//
//   env -i PATH=$PATH HOST=127.0.0.1 DATA_DIR=/tmp/mail-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME="Maya Novak" PORT=18208 ./pocket-mail
//   CDP=http://127.0.0.1:19281 BASE=http://127.0.0.1:18208 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18208'
const shots = process.env.SHOTS !== '0'
const dir = new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 2 : 1, isMobile: phone, hasTouch: phone, colorScheme: 'light',
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1500) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
const snap = async (page, name) => { if (shots) await page.screenshot({ path: dir + name }) }
const firstID = (page, q) => page.evaluate(q => fetch('/api/v1/messages?' + q).then(r => r.json()).then(d => d.messages[0].id), q)

{
  const { ctx, page } = await session()
  // the inbox with a conversation open
  const dinner = await firstID(page, 'q=' + encodeURIComponent('subject:"Dinner on Saturday"'))
  await go(page, `/message/${dinner}`)
  assert.ok(await page.locator('text=Dinner on Saturday?').count(), 'conversation shows')
  await snap(page, 'desktop-inbox.png')
  // photos with attachments
  const photos = await firstID(page, 'q=' + encodeURIComponent('subject:"Photos from the lake"'))
  await go(page, `/message/${photos}`, 2500)
  await snap(page, 'desktop-thread.png')
  // a reply in the open conversation
  await go(page, `/message/${dinner}`)
  await page.getByRole('button', { name: /^reply$/i }).last().click()
  const editor = page.locator('[contenteditable="true"]').first()
  await editor.waitFor({ timeout: 5000 })
  await editor.click()
  await page.keyboard.press('Control+Home')
  await page.keyboard.type('Sounds perfect — see you at 7! We will bring the walnut cake.')
  await page.waitForTimeout(800)
  await snap(page, 'desktop-compose.png')
  await go(page, '/subscriptions')
  await snap(page, 'desktop-subscriptions.png')
  await go(page, '/settings/accounts')
  const add = page.getByRole('button', { name: /add account/i }).first()
  if (await add.count()) { await add.click(); await page.waitForTimeout(500) }
  const email = page.locator('input[type=email]').first()
  if (await email.count()) {
    await email.fill('maya.novak@gmail.com')
    await email.press('Enter')
    await page.waitForTimeout(1500)
  }
  await snap(page, 'desktop-accounts.png')
  await ctx.close()
}
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/')
  await snap(page, 'phone-inbox.png')
  const kickoff = await firstID(page, 'q=kickoff')
  await go(page, `/message/${kickoff}`, 2000)
  await snap(page, 'phone-thread.png')
  await ctx.close()
}
await browser.close()
assert.deepEqual(errors, [], 'no console errors')
console.log(shots ? 'screenshots written to docs/screenshots/' : 'checks passed')
