import { useState } from 'react';
import { clearTokens } from './api/client';
import { Account } from './pages/Account';
import { Activity } from './pages/Activity';
import { Dashboard } from './pages/Dashboard';
import { Login } from './pages/Login';
import { Nodes } from './pages/Nodes';
import { ServerView } from './pages/ServerView';
import { Settings } from './pages/Settings';
import { Users } from './pages/Users';

interface StoredUser {
  id: number;
  email: string;
  username: string;
}

type View = 'servers' | 'nodes' | 'settings' | 'activity' | 'account' | 'users';

function loadUser(): StoredUser | null {
  const raw = localStorage.getItem('user');
  if (!raw) return null;
  try {
    return JSON.parse(raw) as StoredUser;
  } catch {
    return null;
  }
}

export function App() {
  const [user, setUser] = useState<StoredUser | null>(() => loadUser());
  const [view, setView] = useState<View>('servers');
  const [activeServer, setActiveServer] = useState<string | null>(null);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  function handleLogout() {
    clearTokens();
    setUser(null);
  }

  function goTo(next: View) {
    setActiveServer(null);
    setView(next);
    setMobileNavOpen(false);
  }

  if (!user) {
    return <Login onLoggedIn={() => setUser(loadUser())} />;
  }

  return (
    <div id="page-dashboard" className="page active">
      <div className="ambient">
        <div className="blob b1" />
        <div className="blob b2" />
      </div>

      <header className="topbar">
        <button
          className="mobile-nav-toggle"
          onClick={() => setMobileNavOpen((v) => !v)}
          aria-label="Toggle navigation"
        >
          ☰
        </button>
        <div className="topbar-logo">
          <span className="name">Power</span>
          <span className="tag">Node</span>
        </div>
        <div className="topbar-sep" />
        <nav className="breadcrumb">
          <span className={activeServer ? '' : 'bc-cur'} onClick={() => goTo('servers')}>
            {view === 'nodes'
              ? 'Nodes'
              : view === 'settings'
                ? 'Settings'
                : view === 'activity'
                  ? 'Activity'
                  : view === 'account'
                    ? 'Account'
                    : view === 'users'
                      ? 'Users'
                      : 'Dashboard'}
          </span>
          {activeServer && (
            <>
              <span className="bc-sep">/</span>
              <span className="bc-cur">{activeServer.slice(0, 8)}</span>
            </>
          )}
        </nav>
        <div className="topbar-right">
          <div className="user-chip" onClick={() => goTo('account')} style={{ cursor: 'pointer' }}>
            <div className="user-ava">{user.username.slice(0, 1).toUpperCase()}</div>
            <span className="user-name">{user.username}</span>
          </div>
        </div>
      </header>

      <div className="shell-body">
        <div
          className={`sidebar-backdrop ${mobileNavOpen ? 'show' : ''}`}
          onClick={() => setMobileNavOpen(false)}
        />
        <aside className={`sidebar ${mobileNavOpen ? 'mobile-open' : ''}`}>
          <div className="nav-section">
            <div className="nav-section-label">Overview</div>
            <div
              className={`nav-item ${view === 'servers' ? 'active' : ''}`}
              onClick={() => goTo('servers')}
            >
              <span className="nav-icon">▦</span> Servers
            </div>
          </div>
          <div className="nav-section">
            <div className="nav-section-label">Admin</div>
            <div
              className={`nav-item ${view === 'nodes' ? 'active' : ''}`}
              onClick={() => goTo('nodes')}
            >
              <span className="nav-icon">◆</span> Nodes
            </div>
            <div
              className={`nav-item ${view === 'users' ? 'active' : ''}`}
              onClick={() => goTo('users')}
            >
              <span className="nav-icon">☺</span> Users
            </div>
            <div
              className={`nav-item ${view === 'activity' ? 'active' : ''}`}
              onClick={() => goTo('activity')}
            >
              <span className="nav-icon">☰</span> Activity
            </div>
            <div
              className={`nav-item ${view === 'settings' ? 'active' : ''}`}
              onClick={() => goTo('settings')}
            >
              <span className="nav-icon">⚙</span> Settings
            </div>
            <div
              className={`nav-item ${view === 'account' ? 'active' : ''}`}
              onClick={() => goTo('account')}
            >
              <span className="nav-icon">◈</span> Account
            </div>
          </div>
          <div className="sidebar-footer">
            <div className="nav-item logout-item" onClick={handleLogout}>
              <span className="nav-icon">⏻</span> Log out
            </div>
          </div>
        </aside>

        <main className="main">
          {activeServer ? (
            <ServerView uuid={activeServer} onBack={() => setActiveServer(null)} />
          ) : view === 'nodes' ? (
            <Nodes />
          ) : view === 'users' ? (
            <Users />
          ) : view === 'settings' ? (
            <Settings />
          ) : view === 'activity' ? (
            <Activity />
          ) : view === 'account' ? (
            <Account />
          ) : (
            <Dashboard onManage={setActiveServer} />
          )}
        </main>
      </div>
    </div>
  );
}
