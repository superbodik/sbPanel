import type {
  ActivityEntry,
  Allocation,
  ApiKey,
  CreateAllocationRequest,
  CreateApiKeyResponse,
  CreateDatabaseHostRequest,
  CreateNodeRequest,
  CreateNodeResponse,
  CreateScheduleRequest,
  CreateServerRequest,
  CreateUserRequest,
  DatabaseHost,
  Egg,
  FileEntry,
  Node,
  NodeStatus,
  PanelUser,
  PowerAction,
  Schedule,
  Server,
  ServerBackup,
  ServerDatabase,
  ServerDomain,
  SSHKey,
  Subuser,
  TwoFASetup,
  TwoFAStatus,
  UpdateCheck,
  UpdateNodeRequest,
  UpdateUserRequest,
  VersionInfo,
} from '../types';

const API_BASE = '/api/v1';

interface AuthTokens {
  access_token: string;
  refresh_token: string;
  user: { id: number; email: string; username: string };
}

export class TOTPRequiredError extends Error {
  constructor() {
    super('totp code required');
    this.name = 'TOTPRequiredError';
  }
}

function authHeaders(): HeadersInit {
  const token = localStorage.getItem('access_token');
  return token ? { Authorization: `Bearer ${token}` } : {};
}

function storeTokens(tokens: AuthTokens) {
  localStorage.setItem('access_token', tokens.access_token);
  localStorage.setItem('refresh_token', tokens.refresh_token);
  localStorage.setItem('user', JSON.stringify(tokens.user));
}

function clearTokens() {
  localStorage.removeItem('access_token');
  localStorage.removeItem('refresh_token');
  localStorage.removeItem('user');
}

let refreshInFlight: Promise<boolean> | null = null;

async function tryRefresh(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight;

  refreshInFlight = (async () => {
    const refreshToken = localStorage.getItem('refresh_token');
    if (!refreshToken) return false;
    try {
      const res = await fetch(`${API_BASE}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      if (!res.ok) return false;
      storeTokens((await res.json()) as AuthTokens);
      return true;
    } catch {
      return false;
    }
  })();

  const result = await refreshInFlight;
  refreshInFlight = null;
  return result;
}

async function errorMessage(res: Response, path: string, init?: RequestInit): Promise<string> {
  const fallback = `${init?.method ?? 'GET'} ${path} failed: ${res.status}`;
  try {
    const body = (await res.text()).trim();
    return body ? `${body} (${res.status})` : fallback;
  } catch {
    return fallback;
  }
}

async function request<T>(path: string, init?: RequestInit, isRetry = false): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...init?.headers,
    },
  });

  if (res.status === 401) {
    if (!isRetry && (await tryRefresh())) {
      return request<T>(path, init, true);
    }
    clearTokens();
    window.location.reload();
    throw new Error('session expired');
  }
  if (!res.ok) {
    throw new Error(await errorMessage(res, path, init));
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

async function requestText(path: string, init?: RequestInit, isRetry = false): Promise<string> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      ...authHeaders(),
      ...init?.headers,
    },
  });

  if (res.status === 401) {
    if (!isRetry && (await tryRefresh())) {
      return requestText(path, init, true);
    }
    clearTokens();
    window.location.reload();
    throw new Error('session expired');
  }
  if (!res.ok) {
    throw new Error(await errorMessage(res, path, init));
  }
  return res.text();
}

async function requestBlob(path: string, init?: RequestInit, isRetry = false): Promise<Blob> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      ...authHeaders(),
      ...init?.headers,
    },
  });

  if (res.status === 401) {
    if (!isRetry && (await tryRefresh())) {
      return requestBlob(path, init, true);
    }
    clearTokens();
    window.location.reload();
    throw new Error('session expired');
  }
  if (!res.ok) {
    throw new Error(await errorMessage(res, path, init));
  }
  return res.blob();
}

export const api = {
  login: async (email: string, password: string, totpCode?: string) => {
    const res = await fetch(`${API_BASE}/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password, totp_code: totpCode ?? '' }),
    });
    if (res.status === 428) {
      throw new TOTPRequiredError();
    }
    if (!res.ok) {
      throw new Error('invalid credentials');
    }
    return (await res.json()) as AuthTokens;
  },

  listServers: () => request<Server[]>('/servers'),

  getServer: (uuid: string) => request<Server>(`/servers/${uuid}`),

  createServer: (payload: CreateServerRequest) =>
    request<{ id: number; uuid: string }>('/servers', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  power: (uuid: string, action: PowerAction) =>
    request<{ success: boolean; state: string }>(`/servers/${uuid}/power`, {
      method: 'POST',
      body: JSON.stringify({ action }),
    }),

  deleteServer: (uuid: string) => request<void>(`/servers/${uuid}`, { method: 'DELETE' }),

  suspendServer: (uuid: string) => request<void>(`/servers/${uuid}/suspend`, { method: 'POST' }),

  unsuspendServer: (uuid: string) => request<void>(`/servers/${uuid}/unsuspend`, { method: 'POST' }),

  listNodes: () => request<Node[]>('/nodes'),

  createNode: (payload: CreateNodeRequest) =>
    request<CreateNodeResponse>('/nodes', { method: 'POST', body: JSON.stringify(payload) }),

  updateNode: (id: number, payload: UpdateNodeRequest) =>
    request<void>(`/nodes/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  deleteNode: (id: number) => request<void>(`/nodes/${id}`, { method: 'DELETE' }),

  regenerateNodeToken: (id: number) =>
    request<CreateNodeResponse>(`/nodes/${id}/regenerate-token`, { method: 'POST' }),

  checkNodeStatus: (id: number) => request<NodeStatus>(`/nodes/${id}/status`),

  listUsers: () => request<PanelUser[]>('/users'),

  createUser: (payload: CreateUserRequest) =>
    request<{ id: number }>('/users', { method: 'POST', body: JSON.stringify(payload) }),

  updateUser: (id: number, payload: UpdateUserRequest) =>
    request<void>(`/users/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>('/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    }),

  listDatabaseHosts: () => request<DatabaseHost[]>('/database-hosts'),

  createDatabaseHost: (payload: CreateDatabaseHostRequest) =>
    request<{ id: number }>('/database-hosts', { method: 'POST', body: JSON.stringify(payload) }),

  deleteDatabaseHost: (id: number) => request<void>(`/database-hosts/${id}`, { method: 'DELETE' }),

  listServerDatabases: (uuid: string) => request<ServerDatabase[]>(`/servers/${uuid}/databases`),

  createServerDatabase: (uuid: string, databaseHostId: number, name: string) =>
    request<ServerDatabase>(`/servers/${uuid}/databases`, {
      method: 'POST',
      body: JSON.stringify({ database_host_id: databaseHostId, name }),
    }),

  deleteServerDatabase: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/databases/${id}`, { method: 'DELETE' }),

  listServerDomains: (uuid: string) => request<ServerDomain[]>(`/servers/${uuid}/domains`),

  createServerDomain: (uuid: string, domain: string, email: string) =>
    request<ServerDomain>(`/servers/${uuid}/domains`, {
      method: 'POST',
      body: JSON.stringify({ domain, email }),
    }),

  deleteServerDomain: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/domains/${id}`, { method: 'DELETE' }),

  listServerBackups: (uuid: string) => request<ServerBackup[]>(`/servers/${uuid}/backups`),

  createServerBackup: (uuid: string, name: string, ignoredFiles: string[]) =>
    request<ServerBackup>(`/servers/${uuid}/backups`, {
      method: 'POST',
      body: JSON.stringify({ name, ignored_files: ignoredFiles }),
    }),

  restoreServerBackup: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/backups/${id}/restore`, { method: 'POST' }),

  deleteServerBackup: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/backups/${id}`, { method: 'DELETE' }),

  downloadServerBackup: (uuid: string, id: number) =>
    requestBlob(`/servers/${uuid}/backups/${id}/download`),

  getVersion: () => request<VersionInfo>('/version'),

  checkUpdate: () => request<UpdateCheck>('/version/check'),

  listActivity: () => request<ActivityEntry[]>('/activity'),

  listEggs: () => request<Egg[]>('/eggs'),

  listAllocations: (nodeId: number, freeOnly = false) =>
    request<Allocation[]>(`/allocations?node_id=${nodeId}${freeOnly ? '&free=true' : ''}`),

  createAllocation: (payload: CreateAllocationRequest) =>
    request<{ created: number }>('/allocations', { method: 'POST', body: JSON.stringify(payload) }),

  deleteAllocation: (id: number) => request<void>(`/allocations/${id}`, { method: 'DELETE' }),

  listApiKeys: () => request<ApiKey[]>('/account/api-keys'),

  createApiKey: (name: string, permissions: string[]) =>
    request<CreateApiKeyResponse>('/account/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name, permissions }),
    }),

  deleteApiKey: (id: number) => request<void>(`/account/api-keys/${id}`, { method: 'DELETE' }),

  listSSHKeys: () => request<SSHKey[]>('/account/ssh-keys'),

  createSSHKey: (name: string, publicKey: string) =>
    request<SSHKey>('/account/ssh-keys', {
      method: 'POST',
      body: JSON.stringify({ name, public_key: publicKey }),
    }),

  deleteSSHKey: (id: number) => request<void>(`/account/ssh-keys/${id}`, { method: 'DELETE' }),

  get2FAStatus: () => request<TwoFAStatus>('/account/2fa/status'),

  setup2FA: () => request<TwoFASetup>('/account/2fa/setup', { method: 'POST' }),

  verify2FA: (code: string) =>
    request<{ enabled: boolean }>('/account/2fa/verify', {
      method: 'POST',
      body: JSON.stringify({ code }),
    }),

  disable2FA: (password: string) =>
    request<{ enabled: boolean }>('/account/2fa/disable', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),

  listFiles: (uuid: string, path: string) =>
    request<FileEntry[]>(`/servers/${uuid}/files?path=${encodeURIComponent(path)}`),

  readFile: (uuid: string, path: string) =>
    requestText(`/servers/${uuid}/files/contents?path=${encodeURIComponent(path)}`),

  writeFile: (uuid: string, path: string, content: string) =>
    requestText(`/servers/${uuid}/files/contents?path=${encodeURIComponent(path)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/plain' },
      body: content,
    }),

  deleteFile: (uuid: string, path: string) =>
    request<void>(`/servers/${uuid}/files?path=${encodeURIComponent(path)}`, { method: 'DELETE' }),

  createDirectory: (uuid: string, path: string) =>
    request<void>(`/servers/${uuid}/files/directory?path=${encodeURIComponent(path)}`, {
      method: 'POST',
    }),

  renameFile: (uuid: string, from: string, to: string) =>
    request<void>(`/servers/${uuid}/files/rename`, {
      method: 'POST',
      body: JSON.stringify({ from, to }),
    }),

  downloadFile: (uuid: string, path: string) =>
    requestBlob(`/servers/${uuid}/files/contents?path=${encodeURIComponent(path)}`),

  uploadFile: (uuid: string, path: string, file: File) =>
    requestText(`/servers/${uuid}/files/contents?path=${encodeURIComponent(path)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: file,
    }),

  listSchedules: (uuid: string) => request<Schedule[]>(`/servers/${uuid}/schedules`),

  createSchedule: (uuid: string, payload: CreateScheduleRequest) =>
    request<{ id: number }>(`/servers/${uuid}/schedules`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  toggleSchedule: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/schedules/${id}/toggle`, { method: 'POST' }),

  deleteSchedule: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/schedules/${id}`, { method: 'DELETE' }),

  listSubusers: (uuid: string) => request<Subuser[]>(`/servers/${uuid}/subusers`),

  addSubuser: (uuid: string, email: string, permissions: string[]) =>
    request<{ id: number }>(`/servers/${uuid}/subusers`, {
      method: 'POST',
      body: JSON.stringify({ email, permissions }),
    }),

  updateSubuser: (uuid: string, id: number, permissions: string[]) =>
    request<void>(`/servers/${uuid}/subusers/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ permissions }),
    }),

  removeSubuser: (uuid: string, id: number) =>
    request<void>(`/servers/${uuid}/subusers/${id}`, { method: 'DELETE' }),
};

export { storeTokens, clearTokens, tryRefresh };

function wsToken(): string {
  return localStorage.getItem('access_token') ?? '';
}

export function connectServerSocket(uuid: string): WebSocket {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return new WebSocket(`${proto}://${window.location.host}/ws/servers/${uuid}?token=${wsToken()}`);
}

export function connectServerSocketWithRetry<T>(
  uuid: string,
  onMessage: (data: T) => void,
): () => void {
  let closed = false;
  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;

  async function open() {
    if (closed) return;
    if (attempt > 0) await tryRefresh();
    if (closed) return;
    socket = connectServerSocket(uuid);
    socket.onmessage = (event) => {
      try {
        onMessage(JSON.parse(event.data) as T);
        attempt = 0;
      } catch {}
    };
    socket.onclose = () => {
      if (closed) return;
      const delay = Math.min(1000 * 2 ** attempt, 15000);
      attempt += 1;
      retryTimer = setTimeout(open, delay);
    };
  }

  open();

  return () => {
    closed = true;
    if (retryTimer) clearTimeout(retryTimer);
    socket?.close();
  };
}

export function connectConsoleSocket(uuid: string): WebSocket {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return new WebSocket(
    `${proto}://${window.location.host}/ws/servers/${uuid}/console?token=${wsToken()}`,
  );
}

export interface ConsoleHandle {
  send: (data: string) => void;
  close: () => void;
}

export function connectConsoleSocketWithRetry(
  uuid: string,
  onMessage: (line: string) => void,
  onStatusChange: (connected: boolean) => void,
): ConsoleHandle {
  let closed = false;
  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;

  async function open() {
    if (closed) return;
    if (attempt > 0) await tryRefresh();
    if (closed) return;
    socket = connectConsoleSocket(uuid);
    socket.onopen = () => {
      attempt = 0;
      onStatusChange(true);
    };
    socket.onmessage = (event) => onMessage(String(event.data));
    socket.onclose = () => {
      onStatusChange(false);
      if (closed) return;
      const delay = Math.min(1000 * 2 ** attempt, 15000);
      attempt += 1;
      retryTimer = setTimeout(open, delay);
    };
  }

  open();

  return {
    send: (data: string) => {
      if (socket?.readyState === WebSocket.OPEN) socket.send(data);
    },
    close: () => {
      closed = true;
      if (retryTimer) clearTimeout(retryTimer);
      socket?.close();
    },
  };
}
