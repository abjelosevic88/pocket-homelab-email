// Settings → Accounts: your mail accounts, adding one (Sign in with Google/Microsoft or an app password), and the admin's sign-in setup.
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { ACCOUNT_COLORS, accountColor } from '../lib/mail'
import { ago } from '../lib/format'
import type { AccountInput, AccountView, Folder, Preset, Security, Sidebar } from '../lib/types'
import { Icon } from '../components/icons'
import { Confirm, CopyField, Field, Loading, Modal, Segmented, SettingRow, Spinner, Switch, useAsync, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { RichEditor } from '../components/RichEditor'
import { Section, useSettingsForm } from './settings-kit'

const PROVIDER: Record<string, [string, string]> = {
  gmail: ['G', '#ea4335'], google: ['G', '#ea4335'], icloud: ['', '#3b82f6'], outlook: ['O', '#0a64c2'], microsoft: ['O', '#0a64c2'], office365: ['O', '#0a64c2'],
  yahoo: ['Y', '#6001d2'], fastmail: ['F', '#2463eb'], proton: ['P', '#6d4aff'], zoho: ['Z', '#e42527'], gmx: ['G', '#1c449b'], aol: ['A', '#31459b'], demo: ['D', '#64748b'],
}
function ProviderTile({ p, size = 36 }: { p: string; size?: number }) {
  const [ch, bg] = PROVIDER[p] || ['@', '#64748b']
  return <span className="prov-tile" style={{ width: size, height: size, background: bg, fontSize: size * 0.45 }}>{p === 'icloud' ? <Icon name="cloud" size={size * 0.5} /> : ch}</span>
}

function stateLine(a: AccountView): { cls: string; text: string } {
  if (a.sync_state === 'auth') return { cls: 'bad', text: a.sync_error || 'The server did not accept the sign-in. Sign in again.' }
  if (a.sync_state === 'error') return { cls: 'bad', text: a.sync_error || 'The last sync failed. It is tried again by itself.' }
  if (a.status?.syncing || a.sync_state === 'syncing' || a.sync_state === 'new') return { cls: 'busy', text: a.status?.progress || 'Getting your mail…' }
  if (!a.sync_enabled) return { cls: '', text: 'Paused: not synced.' }
  return { cls: 'ok', text: `Up to date${a.last_sync_at ? ` · checked ${ago(a.last_sync_at)}` : ''}${a.status?.idle ? ' · new mail arrives live' : ''}` }
}

export function AccountsTab() {
  const { toast } = useToast()
  const [sp, setSp] = useSearchParams()
  const { data, reload } = useAsync(() => api.get<Sidebar>(`${V1}/sidebar`), [])
  const [adding, setAdding] = useState(sp.get('add') === '1')
  const [edit, setEdit] = useState<AccountView | null>(null)
  const [del, setDel] = useState<AccountView | null>(null)
  const accounts = data?.accounts || []
  const syncing = accounts.some(a => a.status?.syncing || a.sync_state === 'new' || a.sync_state === 'syncing')
  useEffect(() => { if (!syncing) return; const t = setInterval(reload, 3000); return () => clearInterval(t) }, [syncing, reload])
  useEffect(() => {
    const c = sp.get('connected'), e = sp.get('oauth_error')
    if (c) toast('Account connected — your mail is on its way.')
    if (e) toast(`Sign-in did not work: ${e}`, 'err')
    if (c || e || sp.get('add')) { const n = new URLSearchParams(sp); n.delete('connected'); n.delete('oauth_error'); n.delete('add'); setSp(n, { replace: true }); reload() }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const sync = async (a: AccountView) => { try { await api.post(`${V1}/accounts/${a.id}/sync`); toast('Checking for new mail…'); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async (a: AccountView) => { setDel(null); try { await api.del(`${V1}/accounts/${a.id}`); toast('Account removed from Pocket Mail'); reload() } catch (e) { toast((e as Error).message, 'err') } }
  const reconnect = (a: AccountView) => {
    if (a.auth === 'oauth' && a.oauth_provider) window.location.href = `${V1}/oauth/${a.oauth_provider}/start?email=${encodeURIComponent(a.email)}&account_id=${a.id}`
    else setEdit(a)
  }
  return (
    <div className="stack loose">
      <Section title="Your accounts" icon="mail" actions={<button type="button" className="btn primary sm" onClick={() => setAdding(true)}><Icon name="plus" size={14} />Add account</button>}
        sub="Every account shows up in one inbox. Mail stays on your mail server; Pocket Mail keeps a copy of the recent part to be fast and searchable.">
        {!data ? <Loading /> : accounts.length === 0 ? (
          <div className="empty-add">
            <p>No account yet. Add Gmail, iCloud, Outlook or any other address.</p>
            <button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />Add your first account</button>
          </div>
        ) : <div className="acct-list">
          {accounts.map(a => {
            const s = stateLine(a)
            return (
              <div key={a.id} className="acct-item">
                <span className="acct-color" style={{ background: accountColor(a) }} />
                <ProviderTile p={a.provider} />
                <div className="grow">
                  <div className="row"><b className="ellipsis">{a.label || a.email}</b>{a.label && <span className="muted small ellipsis">{a.email}</span>}</div>
                  <div className={`acct-state small ${s.cls}`}>{s.cls === 'busy' && <Spinner size={11} />}{s.cls === 'bad' && <Icon name="alert" size={13} />}{s.text}</div>
                </div>
                <div className="row acct-acts">
                  {(a.sync_state === 'auth') ? <button type="button" className="btn sm primary" onClick={() => reconnect(a)}>Sign in again</button>
                    : <button type="button" className="btn sm ghost" onClick={() => sync(a)} title="Check now"><Icon name="refresh" size={14} /></button>}
                  <button type="button" className="btn sm" onClick={() => setEdit(a)}>Edit</button>
                  <button type="button" className="btn sm ghost danger-text" onClick={() => setDel(a)}>Remove</button>
                </div>
              </div>
            )
          })}
        </div>}
      </Section>
      <SignInSetup />
      {adding && <AddAccount onClose={() => setAdding(false)} onAdded={() => { setAdding(false); reload() }} />}
      {edit && <EditAccount a={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.label || del.email}?`} message="Pocket Mail forgets the account and its copy of the mail. Nothing is deleted on your mail server." onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}

const SEC: { value: Security; label: string }[] = [{ value: 'ssl', label: 'SSL/TLS' }, { value: 'starttls', label: 'STARTTLS' }, { value: 'none', label: 'None' }]

function ServerFields({ f, set }: { f: AccountInput; set: (p: AccountInput) => void }) {
  return (
    <div className="stack">
      <Field label="Login (username)" help="Usually your full address."><input value={f.username || ''} onChange={e => set({ username: e.target.value })} autoComplete="off" /></Field>
      <div className="srv-grid">
        <Field label="Incoming (IMAP) server"><input value={f.imap_host || ''} onChange={e => set({ imap_host: e.target.value })} placeholder="imap.example.com" /></Field>
        <Field label="Port"><input inputMode="numeric" value={f.imap_port || ''} onChange={e => set({ imap_port: Number(e.target.value) || 0 })} /></Field>
        <Field label="Security"><Select ariaLabel="IMAP security" value={f.imap_security || 'ssl'} onChange={v => set({ imap_security: v as Security })} options={SEC} /></Field>
      </div>
      <div className="srv-grid">
        <Field label="Outgoing (SMTP) server"><input value={f.smtp_host || ''} onChange={e => set({ smtp_host: e.target.value })} placeholder="smtp.example.com" /></Field>
        <Field label="Port"><input inputMode="numeric" value={f.smtp_port || ''} onChange={e => set({ smtp_port: Number(e.target.value) || 0 })} /></Field>
        <Field label="Security"><Select ariaLabel="SMTP security" value={f.smtp_security || 'ssl'} onChange={v => set({ smtp_security: v as Security })} options={SEC} /></Field>
      </div>
      <label className="check-row"><Switch size="sm" checked={!!f.saves_sent} onChange={v => set({ saves_sent: v })} label="Server saves sent mail" />The server files sent mail in Sent by itself (Gmail and Outlook do)</label>
    </div>
  )
}

function AddAccount({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const { toast } = useToast()
  const [step, setStep] = useState<'email' | 'provider' | 'password'>('email')
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [p, setP] = useState<Preset | null>(null)
  const [f, setF] = useState<AccountInput>({})
  const [adv, setAdv] = useState(false)
  const [busy, setBusy] = useState('')
  const [test, setTest] = useState<{ ok: boolean; msg: string } | null>(null)
  const set = (patch: AccountInput) => { setF(x => ({ ...x, ...patch })); setTest(null) }
  const discover = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) { toast('Type your full email address.', 'err'); return }
    setBusy('discover')
    try {
      const r = await api.get<Preset>(`${V1}/accounts/discover?email=${encodeURIComponent(email.trim())}`)
      setP(r)
      setF({ email: email.trim(), name: name.trim(), provider: r.provider, username: r.username || email.trim(), imap_host: r.imap_host, imap_port: r.imap_port, imap_security: r.imap_security, smtp_host: r.smtp_host, smtp_port: r.smtp_port, smtp_security: r.smtp_security, saves_sent: r.saves_sent })
      setAdv(!r.imap_host)
      setStep(r.auth.includes('oauth') ? 'provider' : 'password')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const runTest = async () => {
    setBusy('test')
    try { const r = await api.post<{ ok: boolean; error?: string; message?: string }>(`${V1}/accounts/test`, f); setTest({ ok: r.ok, msg: r.ok ? (r.message || 'Signed in to both servers.') : (r.error || 'It did not work.') }) } catch (e) { setTest({ ok: false, msg: (e as Error).message }) } finally { setBusy('') }
  }
  const add = async () => {
    setBusy('add')
    try { await api.post(`${V1}/accounts`, f); toast('Added — your mail is on its way.'); onAdded() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const oauthName = p?.oauth === 'google' ? 'Google' : 'Microsoft'
  return (
    <Modal title="Add a mail account" onClose={onClose} size="lg">
      {step === 'email' && <form onSubmit={discover} className="stack">
        <p className="muted">Type the address. Pocket Mail finds the servers for you.</p>
        <Field label="Email address"><input type="email" value={email} onChange={e => setEmail(e.target.value)} autoFocus placeholder="you@gmail.com" autoComplete="email" /></Field>
        <Field label="Your name" help="What people see as the sender."><input value={name} onChange={e => setName(e.target.value)} placeholder="Ana Marić" autoComplete="name" /></Field>
        <div className="prov-hints">{['gmail', 'icloud', 'outlook', 'yahoo', 'fastmail'].map(x => <ProviderTile key={x} p={x} size={26} />)}<span className="muted small">and any IMAP account</span></div>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy === 'discover'}>{busy === 'discover' ? <Spinner size={13} /> : null}Continue</button></div>
      </form>}
      {step !== 'email' && p && <div className="stack">
        <div className="prov-card">
          <ProviderTile p={p.provider} size={44} />
          <div className="grow"><b>{p.name || 'Your mail server'}</b><div className="muted small">{email}</div></div>
          <button type="button" className="link-btn small" onClick={() => { setStep('email'); setTest(null) }}>Change</button>
        </div>
        {p.help && <p className="small">{p.help}</p>}
        {step === 'provider' && <>
          <button type="button" className="btn primary lg block oauth-btn" disabled={!p.oauth_ready}
            onClick={() => { window.location.href = `${V1}/oauth/${p.oauth}/start?email=${encodeURIComponent(email.trim())}&name=${encodeURIComponent(name.trim())}` }}>
            <ProviderTile p={p.oauth === 'google' ? 'gmail' : 'outlook'} size={22} />Sign in with {oauthName}
          </button>
          {!p.oauth_ready && <div className="callout info small"><Icon name="info" size={14} /><span>An admin needs to set up {oauthName} sign-in first (Settings → Accounts → Sign-in setup).{p.auth.includes('password') ? ' Until then you can use an app password.' : ''}</span></div>}
          {p.auth.includes('password') && <button type="button" className="link-btn small" onClick={() => setStep('password')}>Use an app password instead</button>}
        </>}
        {step === 'password' && <>
          {p.app_password && <div className="callout info small"><Icon name="key" size={14} /><div>
            {p.name || 'This provider'} needs an <b>app password</b> — a separate password just for Pocket Mail, not your normal one. You can remove it any time.
            {p.help_url && <div className="mt-s"><a className="btn sm" href={p.help_url} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Make an app password</a></div>}
          </div></div>}
          <Field label={p.app_password ? 'App password' : 'Password'}><input type="password" value={f.password || ''} onChange={e => set({ password: e.target.value })} autoFocus autoComplete="new-password" /></Field>
          <button type="button" className="conn-advanced" onClick={() => setAdv(a => !a)}>{adv ? 'Hide' : 'Show'} server settings{p.found !== 'builtin' && p.found ? ` (found ${p.found === 'ispdb' ? 'in the Thunderbird list' : p.found === 'mx' ? 'from the domain' : 'by guessing'})` : ''}</button>
          {adv && <ServerFields f={f} set={set} />}
          {test && <div className={`callout ${test.ok ? 'ok' : 'danger'} small`}><Icon name={test.ok ? 'check-circle' : 'alert'} size={14} /><span>{test.msg}</span></div>}
          <div className="form-actions">
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button type="button" className="btn" disabled={!f.password || !!busy} onClick={runTest}>{busy === 'test' ? <Spinner size={13} /> : null}Test</button>
            <button type="button" className="btn primary" disabled={!f.password || !!busy} onClick={add}>{busy === 'add' ? <Spinner size={13} /> : null}Add account</button>
          </div>
        </>}
      </div>}
    </Modal>
  )
}

function EditAccount({ a, onClose, onSaved }: { a: AccountView; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState<AccountInput>({ name: a.name, label: a.label, color: a.color, signature: a.signature, sync_days: a.sync_days, sync_enabled: a.sync_enabled, username: a.username,
    imap_host: a.imap_host, imap_port: a.imap_port, imap_security: a.imap_security, smtp_host: a.smtp_host, smtp_port: a.smtp_port, smtp_security: a.smtp_security, saves_sent: a.saves_sent })
  const [adv, setAdv] = useState(a.sync_state === 'auth' && a.auth === 'password')
  const [busy, setBusy] = useState(false)
  const { data: folders, setData: setFolders } = useAsync(() => api.get<Folder[]>(`${V1}/accounts/${a.id}/folders`), [a.id])
  const set = (p: AccountInput) => setF(x => ({ ...x, ...p }))
  const save = async () => {
    setBusy(true)
    const body = { ...f }
    if (!body.password) delete body.password
    try { await api.put(`${V1}/accounts/${a.id}`, body); toast('Account saved'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const toggleFolder = async (fo: Folder, on: boolean) => {
    setFolders(l => l ? l.map(x => x.id === fo.id ? { ...x, sync: on } : x) : l)
    try { await api.put(`${V1}/folders/${fo.id}`, { sync: on }) } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Modal title={a.label || a.email} onClose={onClose} size="lg" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={busy} onClick={save}>Save</button></>}>
      <div className="stack loose">
        <div className="two-col tight">
          <Field label="Name in the sidebar" help={`Empty = ${a.email}`}><input value={f.label || ''} onChange={e => set({ label: e.target.value })} placeholder="Work, Personal…" /></Field>
          <Field label="Your name on outgoing mail"><input value={f.name || ''} onChange={e => set({ name: e.target.value })} /></Field>
        </div>
        <Field label="Colour" group><div className="row wrap">{ACCOUNT_COLORS.map(c => <button key={c} type="button" className={`swatch ${(f.color || accountColor(a)) === c ? 'active' : ''}`} style={{ background: c }} aria-label={c} onClick={() => set({ color: c })} />)}</div></Field>
        <Field label="Signature" help="Added below new messages and replies from this account.">
          <RichEditor value={f.signature || ''} onChange={h => set({ signature: h })} minHeight={70} placeholder="Ana Marić · +387 61 000 000" ariaLabel="Signature" />
        </Field>
        <SettingRow title="Keep mail here for" help="Older mail stays on the server; search there through the provider.">
          <Segmented size="sm" value={f.sync_days || 90} onChange={v => set({ sync_days: v })} options={[30, 90, 180, 365].map(d => ({ value: d, label: d === 365 ? '1 year' : `${d} days` }))} />
        </SettingRow>
        <SettingRow title="Sync this account" help="Off pauses it: nothing new arrives here."><Switch checked={!!f.sync_enabled} onChange={v => set({ sync_enabled: v })} label="Sync" /></SettingRow>
        {folders && folders.length > 0 && <details className="set-details"><summary>Folders to sync ({folders.filter(x => x.sync).length} of {folders.length})</summary>
          <div className="folder-sync">{folders.filter(x => x.selectable).map(fo => <label key={fo.id} className="check-row"><Switch size="sm" checked={fo.sync} onChange={v => toggleFolder(fo, v)} label={fo.name} /><span className="ellipsis">{fo.display || fo.name}</span>{fo.role && <span className="badge">{fo.role}</span>}</label>)}</div>
        </details>}
        <div>
          <button type="button" className="conn-advanced" onClick={() => setAdv(x => !x)}>{adv ? 'Hide' : 'Advanced:'} sign-in and server settings</button>
          {adv && <div className="stack mt-s">
            {a.auth === 'oauth' ? <p className="muted small">Signed in with {a.oauth_provider === 'google' ? 'Google' : 'Microsoft'}. <a href={`${V1}/oauth/${a.oauth_provider}/start?email=${encodeURIComponent(a.email)}&account_id=${a.id}`}>Sign in again</a></p>
              : <Field label="New password (empty = keep)"><input type="password" value={f.password || ''} onChange={e => set({ password: e.target.value })} autoComplete="new-password" /></Field>}
            <ServerFields f={f} set={set} />
          </div>}
        </div>
      </div>
    </Modal>
  )
}

function SignInSetup() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f || !isAdmin) return null
  const env = !!f.oauth_from_env
  return (
    <Section title="Sign-in setup" icon="key" actions={<span className="row"><span className={`badge ${f.oauth_google ? 'ok' : ''}`}>Google {f.oauth_google ? 'ready' : 'off'}</span><span className={`badge ${f.oauth_microsoft ? 'ok' : ''}`}>Microsoft {f.oauth_microsoft ? 'ready' : 'off'}</span></span>}
      sub="Only for admins, once for the whole box: lets everyone use “Sign in with Google / Microsoft” instead of app passwords. You make the sign-in client yourself; it costs nothing.">
      {env && <div className="callout info small mb"><Icon name="info" size={14} /><span>These come from the server’s environment (GOOGLE_CLIENT_ID, MICROSOFT_CLIENT_ID); change them there.</span></div>}
      <fieldset disabled={env} className="stack loose plain-fieldset">
        <div className="stack">
          <h4>Google (Gmail, Google Workspace)</h4>
          <ol className="steps small">
            <li>Open <a href="https://console.cloud.google.com/apis/credentials" target="_blank" rel="noreferrer">Google Cloud → Credentials</a>, make a project, and switch on the <b>Gmail API</b>.</li>
            <li>OAuth consent screen: External, add yourself as a user, then <b>Publish</b> it (otherwise the sign-in stops working after 7 days).</li>
            <li>Create an <b>OAuth client ID</b> of type “Web application” with this redirect URI:<CopyField value={f.oauth_redirect_google} label="Google redirect URI" /></li>
          </ol>
          <div className="two-col tight">
            <Field label="Client ID"><input value={f.google_client_id || ''} onChange={e => setF({ ...f, google_client_id: e.target.value })} autoComplete="off" /></Field>
            <Field label="Client secret"><input value={f.google_client_secret || ''} onChange={e => setF({ ...f, google_client_secret: e.target.value })} autoComplete="off" /></Field>
          </div>
          <p className="muted xsmall">Google shows a “not verified” warning to your household because the app reads mail; that is expected for a private server.</p>
        </div>
        <div className="stack">
          <h4>Microsoft (Outlook.com, Hotmail, Microsoft 365)</h4>
          <ol className="steps small">
            <li>Open <a href="https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade" target="_blank" rel="noreferrer">Entra → App registrations</a> → New registration, “Accounts in any organizational directory and personal Microsoft accounts”.</li>
            <li>Redirect URI (Web):<CopyField value={f.oauth_redirect_microsoft} label="Microsoft redirect URI" /></li>
            <li>Certificates & secrets → New client secret. API permissions: IMAP.AccessAsUser.All, SMTP.Send, offline_access.</li>
          </ol>
          <div className="three-col tight">
            <Field label="Application (client) ID"><input value={f.microsoft_client_id || ''} onChange={e => setF({ ...f, microsoft_client_id: e.target.value })} autoComplete="off" /></Field>
            <Field label="Client secret"><input value={f.microsoft_client_secret || ''} onChange={e => setF({ ...f, microsoft_client_secret: e.target.value })} autoComplete="off" /></Field>
            <Field label="Tenant" help="common = everyone"><input value={f.microsoft_tenant || ''} onChange={e => setF({ ...f, microsoft_tenant: e.target.value })} placeholder="common" /></Field>
          </div>
        </div>
        <div className="row"><button type="button" className="btn primary" disabled={busy} onClick={() => save()}>Save sign-in setup</button></div>
      </fieldset>
    </Section>
  )
}
