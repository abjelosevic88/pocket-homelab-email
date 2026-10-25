import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, Settings as SettingsT, Sidebar, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { Connection, HOURS, Section, connBadge, useSettingsForm, useTester } from './settings-kit'
import { AccountsTab } from './SettingsAccounts'
import { RulesTab, TemplatesManager } from './SettingsRules'

type TabId = 'accounts' | 'reading' | 'writing' | 'notifications' | 'rules' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'accounts', label: 'Accounts', icon: 'mail', sub: 'Your mail accounts: Gmail, iCloud, Outlook and any other. Add, sign in again, signatures and colours.' },
  { id: 'reading', label: 'Reading', icon: 'eye', sub: 'How the inbox and messages look, images from senders, and what happens after you archive.' },
  { id: 'writing', label: 'Writing', icon: 'edit', sub: 'Undo send, the account you write from, replies and templates.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'A push when mail from people arrives, quiet at night.' },
  { id: 'rules', label: 'Rules', icon: 'sliders', sub: 'What happens to new mail by itself: skip the inbox, star, save attachments to your Pocket apps.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Documents, Files, Tasks, Calendar, Contacts, Notes, Money and your photo library: where your mail can go.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Summaries, quick replies, “catch me up” and help with writing: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export or import your setup.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'accounts'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .select-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'accounts' && <AccountsTab />}
            {tab === 'reading' && <Reading />}
            {tab === 'writing' && <Writing />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'rules' && <RulesTab />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Reading() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="The inbox" icon="inbox">
        <SettingRow title="Conversations" help="Replies stay together in one row, like a chat."><Switch checked={f.conversations} onChange={v => setF({ ...f, conversations: v })} label="Conversations" /></SettingRow>
        <SettingRow title="Inbox tabs" help="People · Updates · Newsletters · Receipts above the inbox, so mail from people is never buried."><Switch checked={f.inbox_categories} onChange={v => setF({ ...f, inbox_categories: v })} label="Inbox tabs" /></SettingRow>
        <SettingRow title="Previews" help="A line of each message under the subject."><Switch checked={f.show_preview} onChange={v => setF({ ...f, show_preview: v })} label="Previews" /></SettingRow>
        <SettingRow title="Density"><Segmented size="sm" value={f.density} onChange={v => setF({ ...f, density: v })} options={[{ value: 'comfortable', label: 'Comfortable' }, { value: 'compact', label: 'Compact' }]} /></SettingRow>
        <SettingRow title="Reading pane" help="On a computer: the message next to the list, or the message instead of the list."><Segmented size="sm" value={f.reading_pane} onChange={v => setF({ ...f, reading_pane: v })} options={[{ value: 'right', label: 'Next to the list' }, { value: 'off', label: 'Full width' }]} /></SettingRow>
      </Section>
      <Section title="Reading" icon="eye">
        <SettingRow title="Mark as read" help="When you open a message.">
          <Select ariaLabel="Mark as read" value={String(f.mark_read_after)} onChange={v => setF({ ...f, mark_read_after: Number(v) })} options={[{ value: '0', label: 'Right away' }, { value: '1', label: 'After 1 second' }, { value: '3', label: 'After 3 seconds' }, { value: '-1', label: 'Never — I mark it myself' }]} />
        </SettingRow>
        <SettingRow title="Images from senders" help="Images loaded from the internet tell the sender that, when and where you read the mail. Asking first keeps that private.">
          <Segmented size="sm" value={f.remote_images} onChange={v => setF({ ...f, remote_images: v })} options={[{ value: 'ask', label: 'Ask first' }, { value: 'always', label: 'Always show' }, { value: 'never', label: 'Never' }]} />
        </SettingRow>
        <SettingRow title="After archive or delete">
          <Segmented size="sm" value={f.after_archive} onChange={v => setF({ ...f, after_archive: v })} options={[{ value: 'next', label: 'Next message' }, { value: 'previous', label: 'Previous' }, { value: 'list', label: 'Back to the list' }]} />
        </SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <div className="two-col tight">
          <Field label="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></Field>
          <Field label="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></Field>
        </div>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Writing() {
  const { f, setF, save, busy } = useSettingsForm()
  const navigate = useNavigate()
  const { data: side } = useAsync(() => api.get<Sidebar>(`${V1}/sidebar`), [])
  if (!f) return <Loading />
  const accts = side?.accounts || []
  return (
    <div className="stack loose">
      <Section title="Sending" icon="send">
        <SettingRow title="Undo send" help="How long a message waits before it really goes, so you can take it back.">
          <Segmented size="sm" value={f.undo_send_seconds} onChange={v => setF({ ...f, undo_send_seconds: v })} options={[0, 5, 10, 20, 30].map(n => ({ value: n, label: n ? `${n} s` : 'Off' }))} />
        </SettingRow>
        <SettingRow title="Write from" help="The account new messages start in. Replies always go from the account the mail came to.">
          <Select ariaLabel="Default account" value={String(f.default_account || '')} onChange={v => setF({ ...f, default_account: Number(v) || 0 })} options={[{ value: '', label: 'The first account' }, ...accts.map(a => ({ value: String(a.id), label: a.label ? `${a.label} (${a.email})` : a.email }))]} />
        </SettingRow>
        <SettingRow title="Reply answers everyone" help="r replies to all the people on the message, not only the sender."><Switch checked={f.reply_all} onChange={v => setF({ ...f, reply_all: v })} label="Reply all" /></SettingRow>
        <SettingRow title="Signatures" help="One per account."><button type="button" className="btn sm" onClick={() => navigate('/settings/accounts')}>Edit in Accounts</button></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
      <TemplatesManager />
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where notifications go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/mail" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="New mail" icon="mail">
        <SettingRow title="Tell me about" help="Newsletters and automatic mail never push, unless a rule says so.">
          <Segmented size="sm" value={f.notify_new} onChange={v => setF({ ...f, notify_new: v })} options={[{ value: 'off', label: 'Nothing' }, { value: 'vip', label: 'VIPs only' }, { value: 'people', label: 'People' }, { value: 'all', label: 'Everything' }]} />
        </SettingRow>
        <SettingRow title="Quiet hours" help="No pushes between these hours (same hour = never quiet).">
          <div className="row"><Select ariaLabel="Quiet from" value={String(f.quiet_from)} onChange={v => setF({ ...f, quiet_from: Number(v) })} options={HOURS} /><span className="muted">to</span><Select ariaLabel="Quiet until" value={String(f.quiet_to)} onChange={v => setF({ ...f, quiet_to: Number(v) })} options={HOURS} /></div>
        </SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        {status('notify')}
      </div>
    </div>
  )
}

const APPS: { slug: string; name: string; icon: string; what: string; help: string; port: string }[] = [
  { slug: 'documents', name: 'Pocket Documents', icon: 'file-text', what: 'Save attachments and whole emails as documents (with OCR and search there), and attach your documents to new mail.', help: 'Pocket Documents → Settings → API tokens', port: '8183' },
  { slug: 'files', name: 'Pocket Files', icon: 'folder', what: 'Save attachments into your files, and attach files from there.', help: 'Pocket Files → Settings → API tokens', port: '8187' },
  { slug: 'tasks', name: 'Pocket Tasks', icon: 'check-square', what: 'Turn an email into a task that links back to it — by hand or with a rule.', help: 'Pocket Tasks → Settings → API tokens', port: '8184' },
  { slug: 'calendar', name: 'Pocket Calendar', icon: 'calendar', what: 'Add invitations and dates from email to your calendar, and see what else is on that day.', help: 'Pocket Calendar → Settings → API tokens', port: '8190' },
  { slug: 'contacts', name: 'Pocket Contacts', icon: 'user', what: 'Addresses as you type, who wrote to you (with their photo), and add new people.', help: 'Pocket Contacts → Settings → API tokens', port: '8191' },
  { slug: 'notes', name: 'Pocket Notes', icon: 'note', what: 'Save an email to your notes.', help: 'Pocket Notes → Settings → API tokens', port: '8182' },
  { slug: 'money', name: 'Pocket Money', icon: 'wallet', what: 'Record a receipt from your email as an expense.', help: 'Pocket Money → Settings → API tokens', port: '8178' },
]

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const str = (k: string) => (f[k] as string) || ''
  const immich = eff(f.immich_mode)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Mail acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      {APPS.map(a => (
        <Section key={a.slug} title={a.name} icon={a.icon} actions={connBadge(eff(str(a.slug + '_mode') as ConnMode || 'off'), str(a.slug + '_via'))} sub={a.what}>
          <Connection app={a.name} mode={(str(a.slug + '_mode') || 'off') as ConnMode} via={str(a.slug + '_via')} detail={str(a.slug + '_detail')} core={coreOn} hasToken={!!str(a.slug + '_token')}
            onMode={m => save({ [a.slug + '_mode']: m })} onDisconnect={() => save({ [a.slug + '_url']: '', [a.slug + '_token']: '' })}
            test={<><button type="button" className="btn sm" onClick={() => run(a.slug, str(a.slug + '_url'), str(a.slug + '_token'))}>Test connection</button>{status(a.slug)}</>}
            direct={<div className="two-col tight">
              <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={str(a.slug + '_url')} onChange={e => setF({ ...f, [a.slug + '_url']: e.target.value })} placeholder={`http://host.docker.internal:${a.port}`} /></Field>
              <Field label="API token" help={a.help}><input value={str(a.slug + '_token')} onChange={e => setF({ ...f, [a.slug + '_token']: e.target.value })} autoComplete="off" /></Field>
            </div>} />
        </Section>
      ))}
      <Section title="Photos — your Immich library" icon="image" actions={immich === 'off' ? <span className="badge">off</span> : <span className="badge ok">{immich === 'core' ? 'Pocket Core' : 'direct'}</span>}
        sub="Save photos people send you straight into your photo library (Pocket Photos shows it).">
        <div className="conn-block">
          <div className="conn-mode">
            <Segmented size="sm" value={immich} onChange={m => save({ immich_mode: m as ConnMode })} options={[
              { value: 'core', label: 'Pocket Core', title: coreOn ? 'Your own Immich key from Pocket Core (My logins)' : 'Needs Pocket Core: there is none on this box', disabled: !coreOn },
              { value: 'direct', label: 'Direct' }, { value: 'off', label: 'Off' }]} />
          </div>
          <div className={`conn-status ${immich === 'off' ? '' : 'ok'}`}><span className="dot" />{immich === 'off' ? 'Off: photos can be downloaded or saved to Files and Documents.' : immich === 'core' ? 'Your own Immich login from Pocket Core (Core → My logins → Immich). Use Test to check it.' : 'Your Immich address and API key.'}</div>
          {immich === 'direct' && <div className="two-col tight">
            <Field label="URL"><input value={f.immich_url} onChange={e => setF({ ...f, immich_url: e.target.value })} placeholder="http://host.docker.internal:8145" /></Field>
            <Field label="API key" help="Immich → Account settings → API keys"><input value={f.immich_key} onChange={e => setF({ ...f, immich_key: e.target.value })} autoComplete="off" /></Field>
          </div>}
          {immich !== 'off' && <div className="row wrap"><button type="button" className="btn sm" onClick={() => run('immich', immich === 'direct' ? f.immich_url : '', immich === 'direct' ? f.immich_key : '')}>Test connection</button>{status('immich')}</div>}
        </div>
      </Section>
      <Section title="Other Pocket apps" icon="layers" sub="Mail offers your email to the rest of the box through Pocket Core, only where you allow it in Core → Links.">
        <dl className="set-dl">
          <div><dt>mail:read</dt><dd className="muted">Search and read your messages — Pocket Day’s journal, Pocket AI answering “what did the plumber say?”. Never your passwords.</dd></div>
          <div><dt>mail:send</dt><dd className="muted">Send from one of your accounts — an invoice from Pocket Invoicing, a book to your Kindle. It lands in Sent like any other.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Summarizes long conversations, suggests short replies, writes or polishes a message from a few words, reads dates and amounts out of mail, and catches you up on what came in. Nothing is sent to the AI until you press a button (quick replies and “catch me up” only for mail from people).">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <Section title="Your preferences" icon="sliders">
        <SettingRow title="Language" help="Replies and summaries in this language. Empty = the language of the email."><input value={f.ai_language} onChange={e => setF({ ...f, ai_language: e.target.value })} placeholder="e.g. English, Srpski" /></SettingRow>
        <SettingRow title="“Catch me up”" help="A small card above the inbox that summarizes unread mail from people when you ask."><Switch checked={f.ai_digest} onChange={v => setF({ ...f, ai_digest: v })} label="Catch me up" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and adds their own mail accounts. Admins manage accounts; they never see anyone else’s mail.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their mail accounts and everything synced here. Nothing is deleted on their mail servers." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You" sub="This is your Pocket account, not a mail account (those are under Accounts).">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read your mail and send from your accounts, so keep it safe. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /counts</code></dt><dd className="muted">Unread in the inbox, starred, snoozed, scheduled — for a Homepage widget.</dd></div>
          <div><dt><code>GET /messages?box=inbox&amp;q=…</code></dt><dd className="muted">List or search messages (the same operators as the search field).</dd></div>
          <div><dt><code>POST /send</code></dt><dd className="muted"><code>{'{"to":[{"addr":"a@b.c"}],"subject":"Hi","body_html":"<p>…</p>"}'}</code> — send from a script or a Shortcut.</dd></div>
          <div><dt><code>GET /summary/YYYY-MM-DD</code></dt><dd className="muted">One day’s mail in numbers — what Pocket Day reads.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')); window.dispatchEvent(new CustomEvent('pml:changed')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-mail.db</code> in the data directory. Keep <code>secret.key</code> next to it: without it the saved mail passwords cannot be opened.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Your setup as one JSON file: accounts (without passwords), rules, templates and your choices about senders. The mail itself stays on your mail servers; download any message as .eml from its More menu → Show original. The people you write with as a CSV.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />People CSV</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Accounts', counts.accounts, 'mail'], ['Messages here', counts.messages, 'inbox'], ['Attachments', counts.attachments, 'paperclip'], ['Rules', counts.rules, 'sliders']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Mail</h3>
          <p className="muted small">All your email in one calm inbox: Gmail, iCloud, Outlook and any IMAP account, with conversations, search, snooze, send later and undo send, one-click unsubscribe, rules, and attachments saved straight to your Pocket apps. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-email" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Documents, Files, Tasks, Calendar, Contacts, Notes, Money, Day and Pocket AI.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
