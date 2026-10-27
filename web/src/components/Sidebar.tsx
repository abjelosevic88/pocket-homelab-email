// Left column: Compose, the unified views with their numbers, smart views, every account with its folders.
import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { useMail } from '../lib/mail-context'
import { BOXES, ROLE_ICON, ROLE_LABEL, accountColor, openCompose } from '../lib/mail'
import type { AccountView, Folder } from '../lib/types'
import { Icon } from './icons'
import { Spinner } from './ui'

const OPEN_KEY = 'pml-sb-open'

export function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  const { status, settings } = useApp()
  const { accounts, counts, loaded } = useMail()
  const navigate = useNavigate()
  const [open, setOpen] = useState<Record<number, boolean>>(() => { try { return JSON.parse(localStorage.getItem(OPEN_KEY) || '{}') } catch { return {} } })
  const toggle = (id: number) => setOpen(o => { const n = { ...o, [id]: !o[id] }; localStorage.setItem(OPEN_KEY, JSON.stringify(n)); return n })
  const title = settings?.title || status?.title || 'Pocket Mail'
  const n = (k?: string) => (k && counts[k] ? counts[k] : 0)
  const nav = (to: string, label: string, icon: string, count?: number) => (
    <NavLink key={to} to={to} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
      <Icon name={icon} size={16} />{label}
      {!!count && <span className="sb-count">{count.toLocaleString()}</span>}
    </NavLink>
  )
  return (
    <aside className="sidebar" aria-label="Mail">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Mail' ? title : <>Pocket <b>Mail</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={() => { onNavigate(); openCompose({ kind: 'new' }) }} title="Compose (c)">
        <Icon name="edit" size={15} strokeWidth={2.1} />Compose<kbd className="kbd-on-primary">C</kbd>
      </button>

      <nav className="sb-nav" aria-label="Views">
        {BOXES.map(b => b.id === 'drafts' ? nav('/drafts', b.label, b.icon, n('drafts'))
          : nav(`/m/${b.id}`, b.label, b.icon, b.id === 'starred' ? undefined : n(b.count)))}
        {(n('scheduled') > 0 || n('failed') > 0) && nav('/scheduled', n('failed') ? 'Not sent' : 'Scheduled', n('failed') ? 'alert' : 'clock', n('scheduled') + n('failed'))}
      </nav>

      <section className="sb-sec" aria-label="Smart views">
        <h4 className="sb-h"><span className="sb-h-btn">Smart views</span></h4>
        <nav className="sb-nav tight">
          {nav('/attachments', 'Attachments', 'paperclip')}
          {nav('/subscriptions', 'Subscriptions', 'rss')}
          {nav('/m/receipts', 'Receipts', 'receipt', n('receipts'))}
          {nav('/m/vip', 'VIP', 'vip')}
        </nav>
      </section>

      <section className="sb-sec" aria-label="Accounts">
        <h4 className="sb-h"><span className="sb-h-btn">Accounts</span>
          <Link to="/settings/accounts?add=1" className="btn ghost icon-only sm" aria-label="Add an account" title="Add an account" onClick={onNavigate}><Icon name="plus" size={14} /></Link>
        </h4>
        {loaded && accounts.length === 0 && (
          <button type="button" className="sb-src-note" onClick={() => { onNavigate(); navigate('/settings/accounts?add=1') }}>
            <Icon name="mail" size={14} /><span>Add your first account: Gmail, iCloud, Outlook or any other.</span>
          </button>
        )}
        <ul className="cal-list">
          {accounts.map(a => <AccountRow key={a.id} a={a} open={!!open[a.id]} onToggle={() => toggle(a.id)} onNavigate={onNavigate} />)}
        </ul>
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pml:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

const ROLE_ORDER = ['inbox', 'flagged', 'drafts', 'sent', 'archive', 'all', 'junk', 'trash', 'important', '']

function AccountRow({ a, open, onToggle, onNavigate }: { a: AccountView; open: boolean; onToggle: () => void; onNavigate: () => void }) {
  const navigate = useNavigate()
  const inbox = a.folders.find(f => f.role === 'inbox')
  const bad = a.sync_state === 'error' || a.sync_state === 'auth'
  const syncing = !!a.status?.syncing || a.sync_state === 'syncing' || a.sync_state === 'new'
  const folders = a.folders.filter(f => f.selectable)
  const top = folders.filter(f => f.role !== '').sort((x, y) => ROLE_ORDER.indexOf(x.role) - ROLE_ORDER.indexOf(y.role))
  const own = folders.filter(f => f.role === '')
  return (
    <li>
      <div className="cal-row sb-row sb-acct">
        <button type="button" className="sb-twisty" aria-label={open ? 'Hide folders' : 'Show folders'} aria-expanded={open} onClick={onToggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} /></button>
        <NavLink to={inbox ? `/f/${inbox.id}` : '/settings/accounts'} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={a.email}>
          <span className="acct-dot" style={{ background: accountColor(a) }} />
          <span className="cal-name ellipsis">{a.label || a.email}</span>
        </NavLink>
        {bad ? <button type="button" className="src-warn btn-plain" title={a.sync_error || 'Sign in again'} aria-label="Account needs attention" onClick={() => { onNavigate(); navigate('/settings/accounts') }}><Icon name="alert" size={14} /></button>
          : syncing ? <span className="sb-sync" title={a.status?.progress || 'Syncing…'}><Spinner size={11} /></span>
          : !!inbox?.unread && <span className="sb-count">{inbox.unread.toLocaleString()}</span>}
      </div>
      {open && <ul className="cal-list sb-folders">
        {[...top, ...own].map(f => <FolderRow key={f.id} f={f} onNavigate={onNavigate} />)}
        {folders.length === 0 && <li className="sb-empty">{syncing ? 'Finding folders…' : 'No folders yet.'}</li>}
      </ul>}
    </li>
  )
}

function FolderRow({ f, onNavigate }: { f: Folder; onNavigate: () => void }) {
  const depth = f.delimiter ? Math.max(0, f.name.split(f.delimiter).length - 1) : 0
  const label = (f.role && ROLE_LABEL[f.role]) || f.display || f.name
  return (
    <li className="cal-row sb-row" style={{ paddingLeft: f.role ? 0 : Math.min(depth, 4) * 10 }}>
      <NavLink to={`/f/${f.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`}>
        <Icon name={ROLE_ICON[f.role] || 'folder'} size={14} className="muted" />
        <span className="cal-name ellipsis">{label}</span>
        {!!f.unread && f.role !== 'sent' && f.role !== 'trash' && <span className="sb-count">{f.unread.toLocaleString()}</span>}
      </NavLink>
    </li>
  )
}
