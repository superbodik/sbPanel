import { useEffect, useMemo, useState } from 'react';
import { api, connectServerSocketWithRetry } from '../api/client';
import type { PowerAction, ResourceStats, Server } from '../types';
import { ServerCard } from './ServerCard';

interface Props {
  onManage: (uuid: string) => void;
}

export function ServerList({ onManage }: Props) {
  const [servers, setServers] = useState<Server[]>([]);
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isAdmin, setIsAdmin] = useState(false);
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkBusy, setBulkBusy] = useState(false);
  const [bulkError, setBulkError] = useState<string | null>(null);

  useEffect(() => {
    api.me().then((me) => setIsAdmin(me.is_admin)).catch(() => {});
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .listServers()
      .then((data) => {
        if (!cancelled) setServers(data);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const closers = servers.map((server) =>
      connectServerSocketWithRetry<ResourceStats>(server.uuid, (stats) => {
        setServers((prev) =>
          prev.map((s) => (s.uuid === stats.server_uuid ? { ...s, live: stats, status: stats.state } : s)),
        );
      }),
    );
    return () => closers.forEach((close) => close());
  }, [servers.map((s) => s.uuid).join(',')]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return servers;
    return servers.filter((s) => s.name.toLowerCase().includes(q) || s.uuid_short.includes(q));
  }, [servers, query]);

  const stats = useMemo(
    () => ({
      total: servers.length,
      online: servers.filter((s) => s.status === 'running').length,
      offline: servers.filter((s) => s.status === 'offline' || s.status === 'suspended').length,
    }),
    [servers],
  );

  async function handlePower(uuid: string, action: PowerAction) {
    setServers((prev) =>
      prev.map((s) => (s.uuid === uuid ? { ...s, status: action === 'stop' ? 'stopping' : 'starting' } : s)),
    );
    try {
      await api.power(uuid, action);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  function toggleSelectMode() {
    setSelectMode((v) => !v);
    setSelected(new Set());
  }

  function toggleSelected(uuid: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(uuid)) next.delete(uuid);
      else next.add(uuid);
      return next;
    });
  }

  async function runBulk(label: string, action: (uuid: string) => Promise<unknown>, confirmMsg?: string) {
    if (selected.size === 0) return;
    if (confirmMsg && !window.confirm(confirmMsg)) return;
    setBulkBusy(true);
    setBulkError(null);
    const uuids = Array.from(selected);
    const results = await Promise.allSettled(uuids.map((uuid) => action(uuid)));
    const failed = results.filter((r) => r.status === 'rejected').length;
    setBulkBusy(false);
    if (failed > 0) {
      setBulkError(`${label}: ${uuids.length - failed} of ${uuids.length} succeeded, ${failed} failed.`);
    }
    setSelected(new Set());
  }

  const bulkPower = (action: PowerAction, confirmMsg?: string) =>
    runBulk(action, (uuid) => api.power(uuid, action), confirmMsg);

  function bulkBackup() {
    const name = `bulk-${new Date().toISOString().replace(/[:.]/g, '-')}`;
    return runBulk('Backup', (uuid) => api.createServerBackup(uuid, name, []));
  }

  function bulkSuspend(suspend: boolean) {
    return runBulk(
      suspend ? 'Suspend' : 'Unsuspend',
      (uuid) => (suspend ? api.suspendServer(uuid) : api.unsuspendServer(uuid)),
      suspend ? `Suspend ${selected.size} server(s)? This stops them and blocks starting until unsuspended.` : undefined,
    );
  }

  if (loading) return <p className="srv-desc">Loading servers…</p>;
  if (error) return <div className="login-error show">{error}</div>;

  return (
    <div>
      <div className="dash-stats">
        <div className="stat-card">
          <div className="stat-card-val">{stats.total}</div>
          <div className="stat-card-lbl">Servers</div>
        </div>
        <div className="stat-card">
          <div className="stat-card-val">{stats.online}</div>
          <div className="stat-card-lbl">Online</div>
        </div>
        <div className="stat-card">
          <div className="stat-card-val">{stats.offline}</div>
          <div className="stat-card-lbl">Offline</div>
        </div>
      </div>

      <div className="dash-toolbar">
        <div className="search-wrap">
          <span className="search-icon">⌕</span>
          <input
            type="text"
            placeholder="Search servers…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        <button className="btn-sm" onClick={toggleSelectMode}>
          {selectMode ? 'Cancel selection' : 'Select servers'}
        </button>
      </div>

      {bulkError && <div className="login-error show" style={{ marginBottom: 16 }}>{bulkError}</div>}

      {selectMode && selected.size > 0 && (
        <div className="dash-toolbar" style={{ flexWrap: 'wrap', gap: 8 }}>
          <span className="srv-desc">{selected.size} selected</span>
          <button className="btn-sm" disabled={bulkBusy} onClick={() => bulkPower('start')}>
            Start
          </button>
          <button className="btn-sm" disabled={bulkBusy} onClick={() => bulkPower('stop')}>
            Stop
          </button>
          <button
            className="btn-sm"
            disabled={bulkBusy}
            onClick={() => bulkPower('restart', `Restart ${selected.size} server(s)?`)}
          >
            Restart
          </button>
          <button className="btn-sm" disabled={bulkBusy} onClick={bulkBackup}>
            Backup
          </button>
          {isAdmin && (
            <>
              <button className="btn-sm" disabled={bulkBusy} onClick={() => bulkSuspend(true)}>
                Suspend
              </button>
              <button className="btn-sm" disabled={bulkBusy} onClick={() => bulkSuspend(false)}>
                Unsuspend
              </button>
            </>
          )}
        </div>
      )}

      <div className="servers-grid">
        {filtered.map((server) => (
          <ServerCard
            key={server.uuid}
            server={server}
            onManage={onManage}
            onPower={handlePower}
            selectable={selectMode}
            selected={selected.has(server.uuid)}
            onToggleSelect={toggleSelected}
          />
        ))}
        {filtered.length === 0 && <p className="srv-desc">No servers match your search.</p>}
      </div>
    </div>
  );
}
