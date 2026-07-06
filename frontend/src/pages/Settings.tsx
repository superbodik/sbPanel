import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { UpdateCheck, VersionInfo } from '../types';

function updateCommand(sourceDir: string): string {
  const dir = sourceDir || '<path to your install.sh checkout>';
  return `cd ${dir} && sudo PANEL_UPDATE=1 ./install.sh`;
}

export function Settings() {
  const [version, setVersion] = useState<VersionInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [updateCheck, setUpdateCheck] = useState<UpdateCheck | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.getVersion().then(setVersion).catch(() => {});
  }, []);

  async function handleCheck() {
    setChecking(true);
    setError(null);
    setUpdateCheck(null);
    try {
      const result = await api.checkUpdate();
      setUpdateCheck(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setChecking(false);
    }
  }

  return (
    <div className="view active">
      <div className="dash-head">
        <h1>Settings</h1>
        <p>Panel version and updates.</p>
      </div>

      <div className="settings-section">
        <div className="settings-card">
          <div className="settings-card-title">Version</div>
          <div className="settings-grid">
            <div className="sfield">
              <label>Version</label>
              <input readOnly value={version ? `v${version.version}` : '…'} />
            </div>
            <div className="sfield">
              <label>Commit</label>
              <input readOnly value={version?.commit ?? '…'} />
            </div>
            <div className="sfield">
              <label>Build date</label>
              <input readOnly value={version?.build_date ?? '…'} />
            </div>
          </div>

          <div className="settings-foot">
            <button className="btn-sm primary" onClick={handleCheck} disabled={checking}>
              {checking ? 'Checking…' : 'Check for Updates'}
            </button>
          </div>

          {error && <div className="login-error show" style={{ marginTop: 12 }}>{error}</div>}

          {updateCheck && (
            <div style={{ marginTop: 16 }}>
              {updateCheck.update_available ? (
                <>
                  <p className="srv-desc" style={{ marginBottom: 8 }}>
                    Update available: <strong>v{updateCheck.current_version}</strong> →{' '}
                    <strong>v{updateCheck.latest_version}</strong>. Run this on the panel's server:
                  </p>
                  <div className="api-item">
                    <span className="api-key">{updateCommand(version?.source_dir ?? '')}</span>
                    <button
                      className="btn-sm"
                      onClick={() =>
                        navigator.clipboard?.writeText(updateCommand(version?.source_dir ?? ''))
                      }
                    >
                      Copy
                    </button>
                  </div>
                </>
              ) : (
                <p className="srv-desc">You're up to date (v{updateCheck.current_version}).</p>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
