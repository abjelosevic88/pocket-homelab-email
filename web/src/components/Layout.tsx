// The app shell: sidebar, top bar with search, live updates, the composer windows, palette, shortcuts.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { MailCtx, useMail, type MailState } from '../lib/mail-context'
import { BOX_LABEL, openCompose, undoLast, useOnChange } from '../lib/mail'
import type { LiveEvent, Sidebar as SidebarT } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ComposerHost } from './Composer'
import { SearchBox } from './SearchBox'
import { Avatar, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery, useToast } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pml-sidebar'

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }

  // ---- the sidebar data, shared with every page ----
  const [side, setSide] = useState<SidebarT | null>(null)
  const loading = useRef(false)
  const again = useRef(false)
  const load = useCallback(() => {
    if (loading.current) { again.current = true; return }
    loading.current = true
    api.get<SidebarT>(`${V1}/sidebar`).then(setSide).catch(() => {}).finally(() => {
      loading.current = false
      if (again.current) { again.current = false; setTimeout(load, 300) }
    })
  }, [])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  // slow refresh while an account is still syncing for the first time
  const syncing = !!side?.accounts.some(a => a.status?.syncing || a.sync_state === 'new' || a.sync_state === 'syncing')
  useEffect(() => { if (!syncing) return; const t = setInterval(load, 4000); return () => clearInterval(t) }, [syncing, load])

  // ---- live updates: one EventSource, re-broadcast as pml:live ----
  useEffect(() => {
    let es: EventSource | null = null
    let timer = 0
    let tries = 0
    let reloadT = 0
    const soon = () => { window.clearTimeout(reloadT); reloadT = window.setTimeout(load, 400) }
    const open = () => {
      es = new EventSource(`${V1}/events`)
      const relay = (type: LiveEvent['type']) => (ev: MessageEvent) => {
        tries = 0
        let data: Record<string, unknown> = {}
        try { data = JSON.parse(ev.data) } catch { /* keep-alive */ }
        const e = { type, ...data } as LiveEvent
        window.dispatchEvent(new CustomEvent('pml:live', { detail: e }))
        soon()
        if (e.type === 'sent' && !e.ok && e.error) toast(`Not sent: ${e.error}`, 'err', { label: 'Open', run: () => navigate('/scheduled') })
      }
      es.addEventListener('change', relay('change'))
      es.addEventListener('sync', relay('sync'))
      es.addEventListener('sent', relay('sent'))
      es.onerror = () => {
        es?.close(); es = null
        tries++
        timer = window.setTimeout(open, Math.min(30000, 1000 * 2 ** Math.min(tries, 5)))
      }
    }
    open()
    return () => { es?.close(); window.clearTimeout(timer); window.clearTimeout(reloadT) }
  }, [load, toast, navigate])

  const mail: MailState = useMemo(() => {
    const accounts = side?.accounts || []
    const mine = new Set(accounts.map(a => a.email.toLowerCase()))
    const folders = new Map(accounts.flatMap(a => a.folders.map(f => [f.id, f] as const)))
    const byId = new Map(accounts.map(a => [a.id, a] as const))
    return { accounts, counts: side?.counts || {}, loaded: !!side, mine, reload: load, folder: id => folders.get(id), account: id => byId.get(id) }
  }, [side, load])

  // the unread count in the tab title
  const { settings } = useApp()
  useEffect(() => {
    const n = mail.counts.inbox || 0
    document.title = `${n ? `(${n}) ` : ''}${settings?.title || 'Pocket Mail'}`
  }, [mail.counts.inbox, settings?.title])

  // ?compose=1 (the PWA shortcut), ?connected / ?oauth_error after a sign-in
  useEffect(() => {
    if (sp.get('compose')) { openCompose({ kind: 'new' }); sp.delete('compose'); setSp(sp, { replace: true }) }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pml:shortcuts', open)
    return () => window.removeEventListener('pml:shortcuts', open)
  }, [])

  // global keys; the mailbox handles j/k/e/r… itself
  const gPending = useRef(0)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"], .composer') || modalOpen() || document.querySelector('.menu, .popover, .select-pop')) return
      const now = Date.now()
      if (now - gPending.current < 1200) {
        gPending.current = 0
        const to: Record<string, string> = { i: '/m/inbox', s: '/m/starred', d: '/drafts', t: '/m/sent', a: '/m/archive', u: '/m/unread', z: '/m/snoozed' }
        if (to[e.key]) { e.preventDefault(); navigate(to[e.key]) }
        return
      }
      switch (e.key) {
        case 'g': gPending.current = now; break
        case 'c': e.preventDefault(); openCompose({ kind: 'new' }); break
        case '/': e.preventDefault(); if (mobile) navigate('/search'); else window.dispatchEvent(new CustomEvent('pml:search')); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'z': e.preventDefault(); undoLast(); break
        case ',': e.preventDefault(); navigate('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'compose', label: 'New message', icon: 'edit', hint: 'C', keywords: 'write compose email send', run: () => openCompose({ kind: 'new' }) },
    { id: 'search', label: 'Search mail', icon: 'search', hint: '/', run: () => mobile ? navigate('/search') : window.dispatchEvent(new CustomEvent('pml:search')) },
    { id: 'inbox', label: 'Inbox', icon: 'inbox', hint: 'G I', run: () => navigate('/m/inbox') },
    { id: 'unread', label: 'Unread', icon: 'mail', run: () => navigate('/m/unread') },
    { id: 'starred', label: 'Starred', icon: 'star', hint: 'G S', run: () => navigate('/m/starred') },
    { id: 'snoozed', label: 'Snoozed', icon: 'alarm', run: () => navigate('/m/snoozed') },
    { id: 'drafts', label: 'Drafts', icon: 'file-text', hint: 'G D', run: () => navigate('/drafts') },
    { id: 'scheduled', label: 'Scheduled and not sent', icon: 'clock', keywords: 'outbox later send', run: () => navigate('/scheduled') },
    { id: 'sent', label: 'Sent', icon: 'send', hint: 'G T', run: () => navigate('/m/sent') },
    { id: 'archive', label: 'Archive', icon: 'archive', run: () => navigate('/m/archive') },
    { id: 'spam', label: 'Spam', icon: 'spam', run: () => navigate('/m/spam') },
    { id: 'trash', label: 'Trash', icon: 'trash', run: () => navigate('/m/trash') },
    { id: 'attachments', label: 'Attachments', icon: 'paperclip', keywords: 'files pdf photos', run: () => navigate('/attachments') },
    { id: 'subs', label: 'Subscriptions and newsletters', icon: 'rss', keywords: 'unsubscribe lists', run: () => navigate('/subscriptions') },
    { id: 'receipts', label: 'Receipts', icon: 'receipt', keywords: 'invoice order payment', run: () => navigate('/m/receipts') },
    ...mail.accounts.map(a => ({ id: `acct${a.id}`, label: `Inbox: ${a.label || a.email}`, icon: 'inbox', keywords: a.email, run: () => { const f = a.folders.find(x => x.role === 'inbox'); navigate(f ? `/f/${f.id}` : '/m/inbox') } })),
    { id: 'add', label: 'Add a mail account', icon: 'plus', keywords: 'gmail icloud outlook imap', run: () => navigate('/settings/accounts?add=1') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'rules', label: 'Settings: rules', icon: 'sliders', keywords: 'filters automatic', run: () => navigate('/settings/rules') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'documents files tasks calendar contacts core', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  // a phone reading one message: the reader brings its own header, no FAB
  const reading = mobile && /^\/(m\/[^/]+|f\/\d+|search)\/\d+/.test(loc.pathname)
  const searchPage = mobile && loc.pathname.startsWith('/search')
  return (
    <MailCtx.Provider value={mail}>
      <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
        {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
        <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
          <Sidebar onNavigate={closeDrawer} />
        </div>
        <div className="main">
          {!reading && !searchPage && <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} />}
          <main className="content mail-content"><Outlet /></main>
        </div>
        {mobile && !reading && <button type="button" className="fab" aria-label="New message" title="New message" onClick={() => openCompose({ kind: 'new' })}><Icon name="edit" size={23} strokeWidth={2.1} /></button>}
        <ComposerHost />
        {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
        {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      </div>
    </MailCtx.Provider>
  )
}

function viewTitle(path: string, mail: MailState): string {
  const parts = path.split('/')
  if (parts[1] === 'm') return BOX_LABEL[parts[2]] || 'Mail'
  if (parts[1] === 'f') {
    const f = mail.folder(Number(parts[2]))
    if (!f) return 'Folder'
    const a = mail.account(f.account_id)
    const name = f.role === 'inbox' ? 'Inbox' : f.display || f.name
    return mail.accounts.length > 1 && a ? `${name} · ${a.label || a.email}` : name
  }
  if (parts[1] === 'search') return 'Search'
  if (parts[1] === 'attachments') return 'Attachments'
  if (parts[1] === 'subscriptions') return 'Subscriptions'
  if (parts[1] === 'scheduled') return 'Scheduled'
  if (parts[1] === 'drafts') return 'Drafts'
  return 'Mail'
}

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const mail = useMail()
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp] = useSearchParams()
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Mail accounts', icon: 'mail', onSelect: () => navigate('/settings/accounts') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pml:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  const q = loc.pathname.startsWith('/search') ? sp.get('q') || '' : ''
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title"><span className="ellipsis">{viewTitle(loc.pathname, mail)}</span></h1>
      {!mobile && <SearchBox initial={q} className="topbar-search" />}
      <span className="spacer" />
      {mobile && <button type="button" className="btn ghost icon-only" onClick={() => navigate('/search')} aria-label="Search" title="Search (/)"><Icon name="search" size={18} /></button>}
      {!mobile && <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Commands" title="Commands (⌘K)"><Icon name="command" size={17} /></button>}
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}

