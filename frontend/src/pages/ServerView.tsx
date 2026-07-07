import { useEffect, useRef, useState } from 'react';
import { api, connectConsoleSocketWithRetry, connectServerSocketWithRetry } from '../api/client';
import type { ConsoleHandle } from '../api/client';
import { BackupManager } from '../components/BackupManager';
import { DatabaseManager } from '../components/DatabaseManager';
import { DomainManager } from '../components/DomainManager';
import { FileManager } from '../components/FileManager';
import { ScheduleManager } from '../components/ScheduleManager';
import { SubuserManager } from '../components/SubuserManager';
import type { PowerAction, ResourceStats, Server } from '../types';

interface Props {
  uuid: string;
  onBack: () => void;
}

type Tab = 'overview' | 'console' | 'files' | 'databases' | 'domains' | 'backups' | 'schedules' | 'sharing';

function pct(used: number, limitMB: number): number {
  const limitBytes = limitMB * 1024 * 1024;
  if (!limitBytes) return 0;
  return Math.min(100, Math.round((used / limitBytes) * 100));
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 MB';
  const mb = bytes / (1024 * 1024);
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb.toFixed(0)} MB`;
}

export function ServerView({ uuid, onBack }: Props) {
  const [server, setServer] = useState<Server | null>(null);
  const [live, setLive] = useState<ResourceStats | null>(null);
  const [tab, setTab] = useState<Tab>('overview');
  const [error, setError] = useState<string | null>(null);
  const [consoleLines, setConsoleLines] = useState<string[]>([]);
  const [consoleConnected, setConsoleConnected] = useState(false);
  const [command, setCommand] = useState('');
  const [deleting, setDeleting] = useState(false);
  const consoleRef = useRef<ConsoleHandle | null>(null);
  const outputRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    api
      .getServer(uuid)
      .then(setServer)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, [uuid]);

  useEffect(() => connectServerSocketWithRetry<ResourceStats>(uuid, setLive), [uuid]);

  useEffect(() => {
    if (tab !== 'console') return;
    setConsoleLines([]);
    setConsoleConnected(false);
    const handle = connectConsoleSocketWithRetry(
      uuid,
      (line) => setConsoleLines((prev) => [...prev.slice(-500), line]),
      setConsoleConnected,
    );
    consoleRef.current = handle;
    return () => {
      handle.close();
      consoleRef.current = null;
    };
  }, [uuid, tab]);

  useEffect(() => {
    outputRef.current?.scrollTo({ top: outputRef.current.scrollHeight });
  }, [consoleLines]);

  async function handlePower(action: PowerAction) {
    try {
      await api.power(uuid, action);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  function sendCommand() {
    if (!command.trim() || !consoleConnected || !consoleRef.current) return;
    consoleRef.current.send(command);
    setCommand('');
  }

  async function handleDelete() {
    if (!window.confirm(`Delete "${server?.name}"? This stops and removes its container. This cannot be undone.`)) {
      return;
    }
    setDeleting(true);
    try {
      await api.deleteServer(uuid);
      onBack();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setDeleting(false);
    }
  }

  if (error) return <div className="login-error show">{error}</div>;
  if (!server) return <p className="srv-desc">Loading…</p>;

  const cpuPct = live ? Math.min(100, Math.round(live.cpu_percent)) : 0;
  const memPct = live ? pct(live.memory_bytes, server.memory_mb) : 0;
  const diskPct = live ? pct(live.disk_bytes, server.disk_mb) : 0;

  return (
    <div className="view active">
      <div className="server-head">
        <span className="bc-sep" onClick={onBack} style={{ cursor: 'pointer' }}>
          ← Back
        </span>
        <h1 style={{ marginTop: 8 }}>{server.name}</h1>
        <p>
          {server.uuid_short} · {server.docker_image}
        </p>
      </div>

      <div style={{ display: 'flex', gap: 24, alignItems: 'flex-start' }}>
        <div style={{ width: 220, flexShrink: 0 }}>
          <div className="power-grid">
            <button className="power-btn start" onClick={() => handlePower('start')}>
              Start
            </button>
            <button className="power-btn stop" onClick={() => handlePower('stop')}>
              Stop
            </button>
            <button className="power-btn" onClick={() => handlePower('restart')}>
              Restart
            </button>
            <button className="power-btn kill" onClick={() => handlePower('kill')}>
              Kill
            </button>
          </div>

          <div className="res-list">
            <div className="res-item">
              <div className="res-head">
                <span>CPU</span>
                <span className="res-val">{live ? `${cpuPct}%` : '—'}</span>
              </div>
              <div className="res-bar">
                <div className="res-bar-fill" style={{ width: `${cpuPct}%` }} />
              </div>
            </div>
            <div className="res-item">
              <div className="res-head">
                <span>RAM</span>
                <span className="res-val">{live ? formatBytes(live.memory_bytes) : '—'}</span>
              </div>
              <div className="res-bar">
                <div className="res-bar-fill" style={{ width: `${memPct}%` }} />
              </div>
            </div>
            <div className="res-item">
              <div className="res-head">
                <span>Disk</span>
                <span className="res-val">{live ? formatBytes(live.disk_bytes) : '—'}</span>
              </div>
              <div className="res-bar">
                <div className="res-bar-fill" style={{ width: `${diskPct}%` }} />
              </div>
            </div>
          </div>
        </div>

        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="tab-bar">
            {(['overview', 'console', 'files', 'databases', 'domains', 'backups', 'schedules', 'sharing'] as Tab[]).map((t) => (
              <div
                key={t}
                className={`tab-btn ${tab === t ? 'active' : ''}`}
                onClick={() => setTab(t)}
              >
                {t.charAt(0).toUpperCase() + t.slice(1)}
              </div>
            ))}
          </div>

          <div className={`tab-panel ${tab === 'overview' ? 'active' : ''}`}>
            <div className="settings-card">
              <div className="settings-card-title">Server info</div>
              <div className="settings-grid">
                <div className="sfield">
                  <label>Status</label>
                  <input readOnly value={live?.state ?? server.status} />
                </div>
                <div className="sfield">
                  <label>Node</label>
                  <input readOnly value={server.node_name ?? '—'} />
                </div>
                <div className="sfield">
                  <label>Address</label>
                  <input readOnly value={server.primary_address ?? 'no allocation assigned'} />
                </div>
                <div className="sfield">
                  <label>Startup command</label>
                  <input readOnly value={server.startup_command} />
                </div>
                <div className="sfield">
                  <label>Memory limit</label>
                  <input readOnly value={`${server.memory_mb} MB`} />
                </div>
                <div className="sfield">
                  <label>Disk limit</label>
                  <input readOnly value={`${server.disk_mb} MB`} />
                </div>
              </div>
            </div>

            <div className="danger-card" style={{ marginTop: 20 }}>
              <div className="danger-row">
                <div className="danger-info">
                  <h3>Delete server</h3>
                  <p>Stops and permanently removes this server's container and data.</p>
                </div>
                <button className="btn-danger" onClick={handleDelete} disabled={deleting}>
                  {deleting ? 'Deleting…' : 'Delete'}
                </button>
              </div>
            </div>
          </div>

          <div className={`tab-panel ${tab === 'console' ? 'active' : ''}`}>
            <div className="console-wrap">
              <div className="console-bar">
                <span className="console-dot r" />
                <span className="console-dot y" />
                <span className="console-dot g" />
                <span className="console-title">{server.name}</span>
                <span className={`console-status ${consoleConnected ? 'online' : ''}`}>
                  {consoleConnected ? 'Connected' : 'Connecting…'}
                </span>
              </div>
              <div className="console-output" ref={outputRef}>
                {consoleLines.map((line, i) => (
                  <div className="con-line" key={i}>
                    <span className="con-msg">{line}</span>
                  </div>
                ))}
                {consoleLines.length === 0 && (
                  <div className="con-line">
                    <span className="con-msg">
                      {consoleConnected ? 'Waiting for output…' : 'Connecting to the node…'}
                    </span>
                  </div>
                )}
              </div>
              <div className="console-input-row">
                <span className="console-prompt">$</span>
                <input
                  className="console-input"
                  value={command}
                  onChange={(e) => setCommand(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') sendCommand();
                  }}
                  placeholder={consoleConnected ? 'Type a command…' : 'Connecting…'}
                  disabled={!consoleConnected}
                />
                <button className="console-send" onClick={sendCommand} disabled={!consoleConnected}>
                  Send
                </button>
              </div>
            </div>
          </div>

          <div className={`tab-panel ${tab === 'files' ? 'active' : ''}`}>
            {tab === 'files' && <FileManager uuid={uuid} />}
          </div>

          <div className={`tab-panel ${tab === 'databases' ? 'active' : ''}`}>
            {tab === 'databases' && <DatabaseManager uuid={uuid} />}
          </div>

          <div className={`tab-panel ${tab === 'domains' ? 'active' : ''}`}>
            {tab === 'domains' && <DomainManager uuid={uuid} />}
          </div>

          <div className={`tab-panel ${tab === 'backups' ? 'active' : ''}`}>
            {tab === 'backups' && <BackupManager uuid={uuid} />}
          </div>

          <div className={`tab-panel ${tab === 'schedules' ? 'active' : ''}`}>
            {tab === 'schedules' && <ScheduleManager uuid={uuid} />}
          </div>

          <div className={`tab-panel ${tab === 'sharing' ? 'active' : ''}`}>
            {tab === 'sharing' && <SubuserManager uuid={uuid} />}
          </div>
        </div>
      </div>
    </div>
  );
}
