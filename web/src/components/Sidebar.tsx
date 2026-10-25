// Left column: start button, sections with today's numbers — the Pocket Calendar sidebar.
import { useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { sessionURL } from '../lib/session'
import type { Dashboard } from '../lib/types'
import { Icon } from './icons'
import { createItems, type CreateKind } from './Layout'
import { type Anchor, Menu, rectOf } from './ui'

export function Sidebar({ onCreate, onNavigate, counts }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; counts: Dashboard | null }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Mail'
  const goal = counts && counts.goal_min > 0 ? `${Math.round(counts.today_min)}/${counts.goal_min} min` : counts && counts.today_min ? `${Math.round(counts.today_min)} min` : undefined
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string; badge?: string }[] = [
    { to: '/', label: 'Today', icon: 'home', end: true, count: goal },
    { to: '/breathe', label: 'Breathe', icon: 'wind' },
    { to: '/meditate', label: 'Meditate', icon: 'lotus' },
    { to: '/path', label: 'The Path', icon: 'mountain', count: counts?.path_total ? `${counts.path_done}/${counts.path_total}` : undefined },
    { to: '/journal', label: 'Journal', icon: 'journal' },
    { to: '/stats', label: 'Statistics', icon: 'chart' },
    { to: '/milestones', label: 'Milestones', icon: 'trophy', count: counts?.level },
  ]
  const go = (url: string) => { onNavigate(); navigate(url) }
  return (
    <aside className="sidebar" aria-label="Still">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Mail' ? title : <>Pocket <b>Still</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="play" size={15} strokeWidth={2.2} />Start<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      {counts && (
        <section className="sb-sec sb-streak" aria-label="Streak">
          <div className="sb-streak-row">
            <Icon name="flame" size={16} className={counts.streak > 0 ? 'flame-on' : 'muted'} />
            <span><b>{counts.streak}</b> {counts.streak === 1 ? 'day' : 'days'} in a row</span>
          </div>
          <div className="sb-streak-sub muted small">{Math.round(counts.week_min)} min this week · best streak {counts.longest_streak}</div>
        </section>
      )}

      <section className="sb-sec">
        <h4 className="sb-h"><span className="sb-h-btn">Quick start</span></h4>
        <div className="sb-quick">
          <button type="button" className="sb-quick-btn" onClick={() => go(sessionURL({ mode: 'breath', technique: 'sigh', minutes: 1 }))}><Icon name="wind" size={14} />1-minute reset</button>
          <button type="button" className="sb-quick-btn" onClick={() => go(sessionURL({ mode: 'breath', technique: 'box' }))}><Icon name="target" size={14} />Box breathing</button>
          <button type="button" className="sb-quick-btn" onClick={() => go(sessionURL({ mode: 'timer', technique: 'silent', minutes: settings?.default_sit_min || 10 }))}><Icon name="timer" size={14} />Silent sit</button>
          <button type="button" className="sb-quick-btn" onClick={() => go(sessionURL({ mode: 'breath', technique: 'relax478' }))}><Icon name="moon" size={14} />For sleep</button>
        </div>
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pml:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
