import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { Allocation, CreateNodeResponse, DatabaseHost, Node, NodeStatus } from '../types';

const INSTALL_SCRIPT_URL = 'https://raw.githubusercontent.com/superbodik/Roost/main/install.sh';

function nodeInstallCommand(daemonToken: string): string {
  return `WINGSD_DAEMON_TOKEN=${daemonToken} WINGSD_PANEL_URL=${window.location.origin} bash <(curl -sSL ${INSTALL_SCRIPT_URL})`;
}

export function Nodes() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [justCreated, setJustCreated] = useState<CreateNodeResponse | null>(null);

  const [form, setForm] = useState({
    name: '',
    fqdn: '',
    scheme: 'http',
    location_id: 1,
    memory_mb: 8192,
    disk_mb: 102400,
  });
  const [submitting, setSubmitting] = useState(false);

  const [allocationNodeId, setAllocationNodeId] = useState(0);
  const [allocations, setAllocations] = useState<Allocation[]>([]);
  const [allocForm, setAllocForm] = useState({ ip: '', port: 25565, portEnd: 25565 });
  const [allocError, setAllocError] = useState<string | null>(null);
  const [allocSubmitting, setAllocSubmitting] = useState(false);

  const [dbHosts, setDbHosts] = useState<DatabaseHost[]>([]);
  const [dbHostForm, setDbHostForm] = useState({
    name: '',
    host: '',
    port: 3306,
    admin_username: 'root',
    admin_password: '',
  });
  const [dbHostError, setDbHostError] = useState<string | null>(null);
  const [dbHostSubmitting, setDbHostSubmitting] = useState(false);

  function refreshDbHosts() {
    api.listDatabaseHosts().then(setDbHosts).catch(() => {});
  }

  async function handleCreateDbHost(e: React.FormEvent) {
    e.preventDefault();
    setDbHostSubmitting(true);
    setDbHostError(null);
    try {
      await api.createDatabaseHost(dbHostForm);
      setDbHostForm((f) => ({ ...f, name: '', host: '', admin_password: '' }));
      refreshDbHosts();
    } catch (err) {
      setDbHostError(err instanceof Error ? err.message : String(err));
    } finally {
      setDbHostSubmitting(false);
    }
  }

  async function handleDeleteDbHost(id: number) {
    if (!window.confirm('Delete this database host? Only possible if nothing is provisioned on it.')) return;
    try {
      await api.deleteDatabaseHost(id);
      refreshDbHosts();
    } catch (err) {
      setDbHostError(err instanceof Error ? err.message : String(err));
    }
  }

  const [statuses, setStatuses] = useState<Record<number, NodeStatus | 'checking'>>({});
  const [expandedNodeId, setExpandedNodeId] = useState<number | null>(null);
  const [deletingNodeId, setDeletingNodeId] = useState<number | null>(null);
  const [editForm, setEditForm] = useState({
    name: '',
    fqdn: '',
    scheme: 'http',
    daemon_port: 8443,
    memory_mb: 0,
    memory_overallocate: 0,
    disk_mb: 0,
    disk_overallocate: 0,
    is_public: true,
    maintenance_mode: false,
  });
  const [savingNodeId, setSavingNodeId] = useState<number | null>(null);
  const [regeneratingNodeId, setRegeneratingNodeId] = useState<number | null>(null);
  const [regeneratedToken, setRegeneratedToken] = useState<CreateNodeResponse | null>(null);

  function toggleExpand(node: Node) {
    if (expandedNodeId === node.id) {
      setExpandedNodeId(null);
      return;
    }
    setExpandedNodeId(node.id);
    setRegeneratedToken(null);
    setEditForm({
      name: node.name,
      fqdn: node.fqdn,
      scheme: node.scheme,
      daemon_port: node.daemon_port,
      memory_mb: node.memory_mb,
      memory_overallocate: node.memory_overallocate,
      disk_mb: node.disk_mb,
      disk_overallocate: node.disk_overallocate,
      is_public: node.is_public,
      maintenance_mode: node.maintenance_mode,
    });
  }

  async function handleRegenerateToken(node: Node) {
    if (!window.confirm(`Generate a new daemon token for "${node.name}"? You'll need to run one command on the node to apply it.`)) {
      return;
    }
    setRegeneratingNodeId(node.id);
    setError(null);
    try {
      setRegeneratedToken(await api.regenerateNodeToken(node.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRegeneratingNodeId(null);
    }
  }

  async function handleSaveNode(node: Node) {
    setSavingNodeId(node.id);
    setError(null);
    try {
      await api.updateNode(node.id, editForm);
      setExpandedNodeId(null);
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingNodeId(null);
    }
  }

  async function handleDeleteNode(node: Node) {
    if (!window.confirm(`Delete node "${node.name}"? This only removes it from the panel — it does not uninstall wingsd from the machine.`)) {
      return;
    }
    setDeletingNodeId(node.id);
    setError(null);
    try {
      await api.deleteNode(node.id);
      setExpandedNodeId(null);
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeletingNodeId(null);
    }
  }

  async function handleCheckStatus(nodeId: number) {
    setStatuses((s) => ({ ...s, [nodeId]: 'checking' }));
    try {
      const status = await api.checkNodeStatus(nodeId);
      setStatuses((s) => ({ ...s, [nodeId]: status }));
    } catch (err) {
      setStatuses((s) => ({
        ...s,
        [nodeId]: { online: false, error: err instanceof Error ? err.message : String(err) },
      }));
    }
  }

  function refreshAllocations(nodeId: number) {
    if (!nodeId) {
      setAllocations([]);
      return;
    }
    api
      .listAllocations(nodeId)
      .then(setAllocations)
      .catch(() => setAllocations([]));
  }

  useEffect(() => {
    refreshAllocations(allocationNodeId);
  }, [allocationNodeId]);

  async function handleCreateAllocation(e: React.FormEvent) {
    e.preventDefault();
    setAllocSubmitting(true);
    setAllocError(null);
    try {
      const result = await api.createAllocation({
        node_id: allocationNodeId,
        ip: allocForm.ip,
        port: allocForm.port,
        port_end: allocForm.portEnd,
      });
      const next = allocForm.portEnd + 1;
      setAllocForm((f) => ({ ...f, port: next, portEnd: next }));
      if (result.created < allocForm.portEnd - allocForm.port + 1) {
        setAllocError(
          `Added ${result.created} of ${allocForm.portEnd - allocForm.port + 1} ports (some already existed).`,
        );
      }
      refreshAllocations(allocationNodeId);
    } catch (err) {
      setAllocError(err instanceof Error ? err.message : String(err));
    } finally {
      setAllocSubmitting(false);
    }
  }

  async function handleDeleteAllocation(id: number) {
    try {
      await api.deleteAllocation(id);
      refreshAllocations(allocationNodeId);
    } catch (err) {
      setAllocError(err instanceof Error ? err.message : String(err));
    }
  }

  function refresh() {
    setLoading(true);
    api
      .listNodes()
      .then(setNodes)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  }

  useEffect(refresh, []);
  useEffect(refreshDbHosts, []);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const created = await api.createNode(form);
      setJustCreated(created);
      setForm((f) => ({ ...f, name: '', fqdn: '' }));
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="view active">
      <div className="dash-head">
        <h1>Nodes</h1>
        <p>Machines running wingsd, ready to host servers.</p>
      </div>

      {justCreated && (
        <div className="acc-card" style={{ marginBottom: 20 }}>
          <div className="acc-card-title">Node created — run this on the node</div>
          <p className="srv-desc" style={{ marginBottom: 10 }}>
            Copy this command and run it on the node's server (as root). It installs Docker
            and wingsd and registers the daemon token automatically — no prompts.
          </p>
          <div className="api-item">
            <span className="api-key">{nodeInstallCommand(justCreated.daemon_token)}</span>
            <button
              className="btn-sm"
              onClick={() => navigator.clipboard?.writeText(nodeInstallCommand(justCreated.daemon_token))}
            >
              Copy
            </button>
          </div>
          <p className="srv-desc" style={{ marginTop: 12, marginBottom: 6 }}>
            Raw token, shown once, in case you're installing manually:
          </p>
          <div className="api-item">
            <span className="api-key">{justCreated.daemon_token}</span>
            <button
              className="btn-sm"
              onClick={() => navigator.clipboard?.writeText(justCreated.daemon_token)}
            >
              Copy
            </button>
          </div>
          <div className="settings-foot">
            <button className="btn-sm" onClick={() => setJustCreated(null)}>
              Done
            </button>
          </div>
        </div>
      )}

      {error && <div className="login-error show" style={{ marginBottom: 16 }}>{error}</div>}

      <div className="settings-card" style={{ marginBottom: 24 }}>
        <div className="settings-card-title">Add node</div>
        <form onSubmit={handleCreate}>
          <div className="settings-grid">
            <div className="sfield">
              <label htmlFor="node-name">Name</label>
              <input
                id="node-name"
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="node-1"
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="node-fqdn">FQDN / IP</label>
              <input
                id="node-fqdn"
                value={form.fqdn}
                onChange={(e) => setForm((f) => ({ ...f, fqdn: e.target.value }))}
                placeholder="node1.example.com"
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="node-scheme">wingsd scheme</label>
              <select
                id="node-scheme"
                value={form.scheme}
                onChange={(e) => setForm((f) => ({ ...f, scheme: e.target.value }))}
              >
                <option value="http">http (default — no TLS cert configured on the node)</option>
                <option value="https">https (only if WINGSD_TLS_CERT/KEY are set)</option>
              </select>
            </div>
            <div className="sfield">
              <label htmlFor="node-memory">Memory (MB)</label>
              <input
                id="node-memory"
                type="number"
                value={form.memory_mb}
                onChange={(e) => setForm((f) => ({ ...f, memory_mb: Number(e.target.value) }))}
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="node-disk">Disk (MB)</label>
              <input
                id="node-disk"
                type="number"
                value={form.disk_mb}
                onChange={(e) => setForm((f) => ({ ...f, disk_mb: Number(e.target.value) }))}
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="node-location">Location ID</label>
              <input
                id="node-location"
                type="number"
                value={form.location_id}
                onChange={(e) => setForm((f) => ({ ...f, location_id: Number(e.target.value) }))}
                required
              />
            </div>
          </div>
          <div className="settings-foot">
            <button className="btn-primary" type="submit" disabled={submitting} style={{ width: 'auto', padding: '10px 20px' }}>
              {submitting ? 'Creating…' : 'Create node'}
            </button>
          </div>
        </form>
      </div>

      {loading ? (
        <p className="srv-desc">Loading nodes…</p>
      ) : (
        <div className="db-table">
          <div className="db-head">
            <span>Name</span>
            <span>Address</span>
            <span>Memory / Disk</span>
            <span>Status</span>
          </div>
          {nodes.map((node) => {
            const status = statuses[node.id];
            const expanded = expandedNodeId === node.id;
            return (
              <div key={node.id}>
                <div className="db-row">
                  <span
                    className="db-name"
                    style={{ cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}
                    onClick={() => toggleExpand(node)}
                  >
                    {node.name}
                    {!node.is_public && (
                      <span className="srv-desc" style={{ fontSize: 10, border: '1px solid var(--border)', borderRadius: 4, padding: '1px 5px' }}>
                        Private
                      </span>
                    )}
                    {node.maintenance_mode && (
                      <span style={{ fontSize: 10, color: 'var(--yellow, #f0b232)', border: '1px solid var(--border)', borderRadius: 4, padding: '1px 5px' }}>
                        Maintenance
                      </span>
                    )}
                  </span>
                  <span className="db-pw">
                    {node.scheme}://{node.fqdn}:{node.daemon_port}
                  </span>
                  <span>{node.memory_mb} MB / {node.disk_mb} MB</span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                    {status === 'checking' ? (
                      'Checking…'
                    ) : status ? (
                      <span
                        title={status.error ?? ''}
                        style={{
                          color: status.online ? 'var(--pink-b)' : '#f23f43',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {status.online ? 'Online' : `Unreachable: ${status.error ?? 'unknown error'}`}
                      </span>
                    ) : (
                      'Unknown'
                    )}
                    <button
                      className="file-act-btn"
                      title="Check connection"
                      onClick={() => handleCheckStatus(node.id)}
                      style={{ flexShrink: 0 }}
                    >
                      ⟳
                    </button>
                  </span>
                </div>
                {expanded && (
                  <div style={{ padding: '14px 18px', borderBottom: '1px solid rgba(192,100,120,.06)' }}>
                    <div className="settings-grid" style={{ marginBottom: 14 }}>
                      <div className="sfield">
                        <label htmlFor={`edit-name-${node.id}`}>Name</label>
                        <input
                          id={`edit-name-${node.id}`}
                          value={editForm.name}
                          onChange={(e) => setEditForm((f) => ({ ...f, name: e.target.value }))}
                        />
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-fqdn-${node.id}`}>FQDN / IP</label>
                        <input
                          id={`edit-fqdn-${node.id}`}
                          value={editForm.fqdn}
                          onChange={(e) => setEditForm((f) => ({ ...f, fqdn: e.target.value }))}
                        />
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-scheme-${node.id}`}>wingsd scheme</label>
                        <select
                          id={`edit-scheme-${node.id}`}
                          value={editForm.scheme}
                          onChange={(e) => setEditForm((f) => ({ ...f, scheme: e.target.value }))}
                        >
                          <option value="http">http (no TLS cert on the node)</option>
                          <option value="https">https (WINGSD_TLS_CERT/KEY set)</option>
                        </select>
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-port-${node.id}`}>Daemon port</label>
                        <input
                          id={`edit-port-${node.id}`}
                          type="number"
                          value={editForm.daemon_port}
                          onChange={(e) =>
                            setEditForm((f) => ({ ...f, daemon_port: Number(e.target.value) }))
                          }
                        />
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-memory-${node.id}`}>Memory (MB)</label>
                        <input
                          id={`edit-memory-${node.id}`}
                          type="number"
                          value={editForm.memory_mb}
                          onChange={(e) =>
                            setEditForm((f) => ({ ...f, memory_mb: Number(e.target.value) }))
                          }
                        />
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-memory-overallocate-${node.id}`}>Memory overallocate (%)</label>
                        <input
                          id={`edit-memory-overallocate-${node.id}`}
                          type="number"
                          value={editForm.memory_overallocate}
                          onChange={(e) =>
                            setEditForm((f) => ({ ...f, memory_overallocate: Number(e.target.value) }))
                          }
                        />
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-disk-${node.id}`}>Disk (MB)</label>
                        <input
                          id={`edit-disk-${node.id}`}
                          type="number"
                          value={editForm.disk_mb}
                          onChange={(e) =>
                            setEditForm((f) => ({ ...f, disk_mb: Number(e.target.value) }))
                          }
                        />
                      </div>
                      <div className="sfield">
                        <label htmlFor={`edit-disk-overallocate-${node.id}`}>Disk overallocate (%)</label>
                        <input
                          id={`edit-disk-overallocate-${node.id}`}
                          type="number"
                          value={editForm.disk_overallocate}
                          onChange={(e) =>
                            setEditForm((f) => ({ ...f, disk_overallocate: Number(e.target.value) }))
                          }
                        />
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: 20, marginBottom: 14 }}>
                      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
                        <div
                          className={`toggle-sw ${editForm.is_public ? 'on' : ''}`}
                          onClick={() => setEditForm((f) => ({ ...f, is_public: !f.is_public }))}
                        >
                          <div className="toggle-knob" />
                        </div>
                        Public (visible to all users when creating a server)
                      </label>
                      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
                        <div
                          className={`toggle-sw ${editForm.maintenance_mode ? 'on' : ''}`}
                          onClick={() => setEditForm((f) => ({ ...f, maintenance_mode: !f.maintenance_mode }))}
                        >
                          <div className="toggle-knob" />
                        </div>
                        Maintenance mode (blocks new servers)
                      </label>
                    </div>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                      <button
                        className="btn-primary"
                        style={{ width: 'auto', padding: '8px 16px' }}
                        disabled={savingNodeId === node.id}
                        onClick={() => handleSaveNode(node)}
                      >
                        {savingNodeId === node.id ? 'Saving…' : 'Save'}
                      </button>
                      <button
                        className="btn-sm"
                        disabled={regeneratingNodeId === node.id}
                        onClick={() => handleRegenerateToken(node)}
                      >
                        {regeneratingNodeId === node.id ? 'Generating…' : 'Regenerate token'}
                      </button>
                      <button
                        className="btn-danger"
                        style={{ width: 'auto', padding: '8px 16px' }}
                        disabled={deletingNodeId === node.id}
                        onClick={() => handleDeleteNode(node)}
                      >
                        {deletingNodeId === node.id ? 'Deleting…' : 'Delete node'}
                      </button>
                    </div>

                    {regeneratedToken && regeneratedToken.id === node.id && (
                      <div style={{ marginTop: 14 }}>
                        <p className="srv-desc" style={{ marginBottom: 8 }}>
                          New token generated. Run this on the node to apply it —
                          it updates the existing wingsd install and restarts it,
                          nothing else changes.
                        </p>
                        <div className="api-item">
                          <span className="api-key">{nodeInstallCommand(regeneratedToken.daemon_token)}</span>
                          <button
                            className="btn-sm"
                            onClick={() =>
                              navigator.clipboard?.writeText(nodeInstallCommand(regeneratedToken.daemon_token))
                            }
                          >
                            Copy
                          </button>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
          {nodes.length === 0 && <p className="srv-desc" style={{ padding: 16 }}>No nodes yet.</p>}
        </div>
      )}

      <div className="settings-card" style={{ marginTop: 24 }}>
        <div className="settings-card-title">Allocations</div>
        <div className="settings-grid" style={{ marginBottom: 16 }}>
          <div className="sfield">
            <label htmlFor="alloc-node">Node</label>
            <select
              id="alloc-node"
              value={allocationNodeId}
              onChange={(e) => setAllocationNodeId(Number(e.target.value))}
            >
              <option value={0} disabled>
                Select a node…
              </option>
              {nodes.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name}
                </option>
              ))}
            </select>
          </div>
        </div>

        {allocationNodeId > 0 && (
          <>
            <form onSubmit={handleCreateAllocation}>
              <div className="settings-grid">
                <div className="sfield">
                  <label htmlFor="alloc-ip">IP</label>
                  <input
                    id="alloc-ip"
                    value={allocForm.ip}
                    onChange={(e) => setAllocForm((f) => ({ ...f, ip: e.target.value }))}
                    placeholder="node's public IP"
                    required
                  />
                </div>
                <div className="sfield">
                  <label htmlFor="alloc-port">Port (start)</label>
                  <input
                    id="alloc-port"
                    type="number"
                    value={allocForm.port}
                    onChange={(e) => {
                      const port = Number(e.target.value);
                      setAllocForm((f) => ({
                        ...f,
                        port,
                        portEnd: f.portEnd === f.port ? port : f.portEnd,
                      }));
                    }}
                    required
                  />
                </div>
                <div className="sfield">
                  <label htmlFor="alloc-port-end">Port (end, optional range)</label>
                  <input
                    id="alloc-port-end"
                    type="number"
                    value={allocForm.portEnd}
                    onChange={(e) => setAllocForm((f) => ({ ...f, portEnd: Number(e.target.value) }))}
                    required
                  />
                </div>
              </div>
              {allocError && <div className="login-error show" style={{ marginTop: 12 }}>{allocError}</div>}
              <div className="settings-foot">
                <button
                  className="btn-sm primary"
                  type="submit"
                  disabled={allocSubmitting}
                >
                  {allocSubmitting ? 'Adding…' : 'Add allocation(s)'}
                </button>
              </div>
            </form>

            <div className="db-table" style={{ marginTop: 16 }}>
              <div className="db-head">
                <span>Address</span>
                <span>Status</span>
                <span />
                <span />
              </div>
              {allocations.map((a) => (
                <div className="db-row" key={a.id}>
                  <span className="db-name">
                    {a.ip}:{a.port}
                  </span>
                  <span>{a.server_id ? 'In use' : 'Free'}</span>
                  <span />
                  <span>
                    {!a.server_id && (
                      <button className="file-act-btn del" onClick={() => handleDeleteAllocation(a.id)}>
                        Delete
                      </button>
                    )}
                  </span>
                </div>
              ))}
              {allocations.length === 0 && (
                <p className="srv-desc" style={{ padding: 16 }}>
                  No allocations on this node yet.
                </p>
              )}
            </div>
          </>
        )}
      </div>

      <div className="settings-card" style={{ marginTop: 24 }}>
        <div className="settings-card-title">Database hosts</div>
        <p className="srv-desc" style={{ marginBottom: 14 }}>
          Register a reachable MySQL/MariaDB server here so users can provision
          per-server databases from the Databases tab. The admin credentials need
          CREATE/DROP DATABASE, CREATE/DROP USER, and GRANT privileges.
        </p>
        <form onSubmit={handleCreateDbHost}>
          <div className="settings-grid">
            <div className="sfield">
              <label htmlFor="dbhost-name">Name</label>
              <input
                id="dbhost-name"
                value={dbHostForm.name}
                onChange={(e) => setDbHostForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="main-mysql"
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="dbhost-host">Host</label>
              <input
                id="dbhost-host"
                value={dbHostForm.host}
                onChange={(e) => setDbHostForm((f) => ({ ...f, host: e.target.value }))}
                placeholder="127.0.0.1 or a domain"
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="dbhost-port">Port</label>
              <input
                id="dbhost-port"
                type="number"
                value={dbHostForm.port}
                onChange={(e) => setDbHostForm((f) => ({ ...f, port: Number(e.target.value) }))}
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="dbhost-user">Admin username</label>
              <input
                id="dbhost-user"
                value={dbHostForm.admin_username}
                onChange={(e) => setDbHostForm((f) => ({ ...f, admin_username: e.target.value }))}
                required
              />
            </div>
            <div className="sfield">
              <label htmlFor="dbhost-pass">Admin password</label>
              <input
                id="dbhost-pass"
                type="password"
                value={dbHostForm.admin_password}
                onChange={(e) => setDbHostForm((f) => ({ ...f, admin_password: e.target.value }))}
                required
              />
            </div>
          </div>
          {dbHostError && <div className="login-error show" style={{ marginTop: 12 }}>{dbHostError}</div>}
          <div className="settings-foot">
            <button className="btn-sm primary" type="submit" disabled={dbHostSubmitting}>
              {dbHostSubmitting ? 'Adding…' : 'Add database host'}
            </button>
          </div>
        </form>

        <div className="db-table" style={{ marginTop: 16 }}>
          <div className="db-head">
            <span>Name</span>
            <span>Address</span>
            <span>Admin user</span>
            <span />
          </div>
          {dbHosts.map((host) => (
            <div className="db-row" key={host.id}>
              <span className="db-name">{host.name}</span>
              <span className="db-pw">{host.host}:{host.port}</span>
              <span>{host.admin_username}</span>
              <span>
                <button className="file-act-btn del" onClick={() => handleDeleteDbHost(host.id)}>
                  Delete
                </button>
              </span>
            </div>
          ))}
          {dbHosts.length === 0 && (
            <p className="srv-desc" style={{ padding: 16 }}>
              No database hosts registered yet.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
