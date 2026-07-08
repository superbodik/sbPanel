import { useState } from 'react';
import { api, storeTokens, TOTPRequiredError } from '../api/client';

interface Props {
  onLoggedIn: () => void;
}

export function Login({ onLoggedIn }: Props) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [needsTotp, setNeedsTotp] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const tokens = await api.login(email, password, needsTotp ? totpCode : undefined);
      storeTokens(tokens);
      onLoggedIn();
    } catch (err) {
      if (err instanceof TOTPRequiredError) {
        setNeedsTotp(true);
      } else {
        setError(needsTotp ? 'Invalid email, password, or code' : 'Invalid email or password');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div id="page-login" className="page active">
      <div className="ambient">
        <div className="blob b1" />
        <div className="blob b2" />
      </div>

      <div className="login-box">
        <div className="login-logo">
          <div className="login-logo-text">
            <div className="title">Power</div>
            <div className="sub">Node</div>
          </div>
        </div>

        {!needsTotp ? (
          <div className="login-step" key="credentials">
            <div className="login-head">
              <h1>Sign in</h1>
              <p>Use your PowerNode account to continue.</p>
            </div>

            <form onSubmit={handleSubmit}>
              <div className="form-field">
                <label htmlFor="email">Email</label>
                <input
                  id="email"
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoFocus
                  required
                />
              </div>
              <div className="form-field">
                <label htmlFor="password">Password</label>
                <input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
              </div>

              <button className="btn-primary" type="submit" disabled={submitting}>
                {submitting ? 'Signing in…' : 'Sign in'}
              </button>

              {error && <div className="login-error show">{error}</div>}
            </form>
          </div>
        ) : (
          <div className="login-step" key="totp">
            <div className="login-head">
              <h1>Verification code</h1>
              <p>Enter the 6-digit code from your authenticator app.</p>
            </div>

            <form onSubmit={handleSubmit}>
              <div className="form-field">
                <label htmlFor="totp-code">Authenticator code</label>
                <input
                  id="totp-code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  value={totpCode}
                  onChange={(e) => setTotpCode(e.target.value)}
                  placeholder="123456"
                  autoFocus
                  required
                />
              </div>

              <button className="btn-primary" type="submit" disabled={submitting}>
                {submitting ? 'Verifying…' : 'Verify & sign in'}
              </button>

              {error && <div className="login-error show">{error}</div>}

              <button
                type="button"
                className="login-back"
                onClick={() => {
                  setNeedsTotp(false);
                  setTotpCode('');
                  setError(null);
                }}
              >
                ← Back
              </button>
            </form>
          </div>
        )}
      </div>
    </div>
  );
}
