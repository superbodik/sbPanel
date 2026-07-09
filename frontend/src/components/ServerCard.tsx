import type { PowerAction, Server } from '../types';
import { StatusBadge } from './StatusBadge';

interface Props {
  server: Server;
  onManage: (uuid: string) => void;
  onPower: (uuid: string, action: PowerAction) => void;
  selectable?: boolean;
  selected?: boolean;
  onToggleSelect?: (uuid: string) => void;
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 MB';
  const mb = bytes / (1024 * 1024);
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb.toFixed(0)} MB`;
}

function pct(used: number, limitMB: number): number {
  const limitBytes = limitMB * 1024 * 1024;
  if (!limitBytes) return 0;
  return Math.min(100, Math.round((used / limitBytes) * 100));
}

export function ServerCard({ server, onManage, onPower, selectable, selected, onToggleSelect }: Props) {
  const live = server.live;
  const cpuPct = live ? Math.min(100, Math.round(live.cpu_percent)) : 0;
  const memPct = live ? pct(live.memory_bytes, server.memory_mb) : 0;
  const diskPct = live ? pct(live.disk_bytes, server.disk_mb) : 0;

  return (
    <div
      className="srv-card"
      onClick={selectable ? () => onToggleSelect?.(server.uuid) : undefined}
      style={selectable ? { cursor: 'pointer', outline: selected ? '2px solid var(--pink-b)' : 'none' } : undefined}
    >
      <div className="srv-card-head">
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
          {selectable && (
            <input
              type="checkbox"
              checked={!!selected}
              onChange={() => onToggleSelect?.(server.uuid)}
              onClick={(e) => e.stopPropagation()}
              style={{ marginTop: 4 }}
            />
          )}
          <div>
            <div className="srv-name">{server.name}</div>
            {server.description && <div className="srv-desc">{server.description}</div>}
            {server.node_name && <div className="srv-node">{server.node_name}</div>}
          </div>
        </div>
        <StatusBadge status={server.status} />
      </div>

      {server.primary_address && <div className="srv-addr">{server.primary_address}</div>}

      <div className="srv-resources">
        <ResRow label="CPU" pct={cpuPct} value={live ? `${cpuPct}%` : '—'} />
        <ResRow label="RAM" pct={memPct} value={live ? formatBytes(live.memory_bytes) : '—'} />
        <ResRow label="Disk" pct={diskPct} value={live ? formatBytes(live.disk_bytes) : '—'} />
      </div>

      <div className="srv-card-foot" onClick={(e) => e.stopPropagation()}>
        <button className="btn-manage" onClick={() => onManage(server.uuid)}>
          Manage
        </button>
        {server.status === 'running' ? (
          <button className="btn-icon" title="Stop" onClick={() => onPower(server.uuid, 'stop')}>
            ■
          </button>
        ) : (
          <button className="btn-icon" title="Start" onClick={() => onPower(server.uuid, 'start')}>
            ▶
          </button>
        )}
        <button className="btn-icon" title="Restart" onClick={() => onPower(server.uuid, 'restart')}>
          ⟳
        </button>
      </div>
    </div>
  );
}

function ResRow({ label, pct, value }: { label: string; pct: number; value: string }) {
  return (
    <div className="srv-res-row">
      <span className="srv-res-lbl">{label}</span>
      <div className="srv-res-bar">
        <div className="srv-res-fill" style={{ width: `${pct}%` }} />
      </div>
      <span className="srv-res-val">{value}</span>
    </div>
  );
}
