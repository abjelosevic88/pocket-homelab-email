import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/mail.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Mailbox from './pages/Mailbox'
import Attachments from './pages/Attachments'
import Subscriptions from './pages/Subscriptions'
import Outbox from './pages/Outbox'
import OpenMessage from './pages/OpenMessage'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="mail"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<Navigate to="/m/inbox" replace />} />
        <Route path="/m/:box" element={<Mailbox />} />
        <Route path="/m/:box/:id" element={<Mailbox />} />
        <Route path="/f/:folder" element={<Mailbox />} />
        <Route path="/f/:folder/:id" element={<Mailbox />} />
        <Route path="/search" element={<Mailbox />} />
        <Route path="/search/:id" element={<Mailbox />} />
        <Route path="/message/:id" element={<OpenMessage />} />
        <Route path="/attachments" element={<Attachments />} />
        <Route path="/subscriptions" element={<Subscriptions />} />
        <Route path="/scheduled" element={<Outbox kind="scheduled" />} />
        <Route path="/drafts" element={<Outbox kind="drafts" />} />
        <Route path="*" element={<Navigate to="/m/inbox" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
