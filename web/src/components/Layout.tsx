// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Dashboard, Library } from '../lib/types'
import { sessionURL } from '../lib/session'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { PracticeModal } from './PracticeForm'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pml-sidebar'

/** Something was added or changed from the shell (or a page): the counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pml:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pml:changed', h)
    return () => window.removeEventListener('pml:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

/** The library (patterns, styles, your presets), for every page that needs it; reload() after a change. */
const LibraryCtx = createContext<{ library: Library | null; reload: () => void }>({ library: null, reload: () => {} })
export const useLibrary = () => useContext(LibraryCtx)

export type CreateKind = 'reset' | 'breathe' | 'sit' | 'path' | 'practice'

/** Open a create action from anywhere (a page's button); "practice" opens the add-a-practice dialog. */
export const openCreate = (k: CreateKind) => window.dispatchEvent(new CustomEvent('pml:create', { detail: { kind: k } }))

/** The start menu: the sidebar's "Start" button and the phone's + button. */
export function createItems(onCreate: (k: CreateKind) => void): MenuItem[] {
  return [
    { label: 'One-minute reset', icon: 'wind', hint: '1', onSelect: () => onCreate('reset') },
    { label: 'Breathe…', icon: 'wind', hint: 'B', onSelect: () => onCreate('breathe') },
    { label: 'Meditate…', icon: 'lotus', hint: 'M', onSelect: () => onCreate('sit') },
    { label: 'Continue the Path', icon: 'mountain', hint: 'P', onSelect: () => onCreate('path') },
    'sep',
    { label: 'Add a practice by hand', icon: 'plus', hint: 'N', onSelect: () => onCreate('practice') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  const [counts, setCounts] = useState<Dashboard | null>(null)
  const [library, setLibrary] = useState<Library | null>(null)
  const load = useCallback(() => { api.get<Dashboard>(`${V1}/dashboard`).then(setCounts).catch(() => {}) }, [])
  const loadLibrary = useCallback(() => { api.get<Library>(`${V1}/library`).then(setLibrary).catch(() => {}) }, [])
  useEffect(() => { load() }, [loc.pathname, load])
  useEffect(() => { loadLibrary() }, [loadLibrary])
  useOnChange(() => { load(); loadLibrary() })

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [modal, setModal] = useState<{ kind: 'practice' } | null>(null)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  // /?add=1 (the PWA shortcut) opens the add-a-practice dialog
  useEffect(() => { if (sp.get('add')) { setModal({ kind: 'practice' }); setSp(new URLSearchParams(), { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const create = (k: CreateKind) => {
    closeDrawer()
    switch (k) {
      case 'reset': navigate(sessionURL({ mode: 'breath', technique: 'sigh', minutes: 1 })); break
      case 'breathe': navigate('/breathe'); break
      case 'sit': navigate('/meditate'); break
      case 'path': navigate('/path'); break
      case 'practice': setModal({ kind: 'practice' }); break
    }
  }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = (e: Event) => { const d = (e as CustomEvent<{ kind: CreateKind }>).detail; createRef.current(d.kind) }
    window.addEventListener('pml:shortcuts', open)
    window.addEventListener('pml:create', make)
    return () => { window.removeEventListener('pml:shortcuts', open); window.removeEventListener('pml:create', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const mk = (k: CreateKind) => { e.preventDefault(); createRef.current(k) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': mk('practice'); break
        case '1': mk('reset'); break
        case 't': go(sessionURL({ mode: 'timer', technique: 'silent', minutes: 10 })); break
        case 'h': go('/'); break
        case 'b': go('/breathe'); break
        case 'm': go('/meditate'); break
        case 'p': go('/path'); break
        case 'j': go('/journal'); break
        case 's': go('/stats'); break
        case 'g': go('/milestones'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'reset', label: 'One-minute reset', icon: 'wind', hint: '1', keywords: 'sigh quick calm stress', run: () => create('reset') },
    { id: 'sit', label: 'Silent sit, 10 minutes', icon: 'timer', hint: 'T', keywords: 'meditate timer bells', run: () => navigate(sessionURL({ mode: 'timer', technique: 'silent', minutes: 10 })) },
    { id: 'add', label: 'Add a practice by hand', icon: 'plus', hint: 'N', keywords: 'log manual record', run: () => create('practice') },
    { id: 'home', label: 'Today', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'breathe', label: 'Breathe', icon: 'wind', hint: 'B', keywords: 'patterns box 478 coherent', run: () => navigate('/breathe') },
    { id: 'meditate', label: 'Meditate', icon: 'lotus', hint: 'M', keywords: 'timer bells styles body scan', run: () => navigate('/meditate') },
    { id: 'path', label: 'The Path and courses', icon: 'mountain', hint: 'P', keywords: 'stages lessons learn course', run: () => navigate('/path') },
    { id: 'journal', label: 'Journal', icon: 'journal', hint: 'J', keywords: 'history practices notes', run: () => navigate('/journal') },
    { id: 'stats', label: 'Statistics', icon: 'chart', hint: 'S', keywords: 'stats streak minutes heatmap insights', run: () => navigate('/stats') },
    { id: 'milestones', label: 'Milestones and levels', icon: 'trophy', hint: 'G', keywords: 'achievements badges level', run: () => navigate('/milestones') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'practice', label: 'Settings: practice and sounds', icon: 'bell', keywords: 'goal bells voice haptics reminders', run: () => navigate('/settings/practice') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'habits health navidrome music core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  const closeModal = () => setModal(null)
  const done = () => { closeModal(); announceChange() }
  return (
    <CrumbCtx.Provider value={setCrumb}>
      <LibraryCtx.Provider value={{ library, reload: loadLibrary }}>
        <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
          {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
          <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
            <Sidebar onCreate={create} onNavigate={closeDrawer} counts={counts} />
          </div>
          <div className="main">
            <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
            <main className="content"><div className="content-inner"><Outlet /></div></main>
          </div>
          {mobile && <button type="button" className="fab" aria-label="Start" title="Start" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="play" size={24} strokeWidth={2.2} /></button>}
          {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(create)} onClose={() => setFabMenu(null)} width={250} />}
          {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} library={library} />}
          {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
          {modal?.kind === 'practice' && <PracticeModal onClose={closeModal} onSaved={p => { done(); navigate(`/journal/${p.id}`) }} />}
        </div>
      </LibraryCtx.Provider>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Today', '/'], breathe: ['Breathe', '/breathe'], meditate: ['Meditate', '/meditate'], path: ['The Path', '/path'], journal: ['Journal', '/journal'], stats: ['Statistics', '/stats'], milestones: ['Milestones', '/milestones'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pml:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
