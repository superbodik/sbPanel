export type ServerStatus =
  | 'installing'
  | 'install_failed'
  | 'suspended'
  | 'offline'
  | 'starting'
  | 'running'
  | 'stopping';

export interface Server {
  id: number;
  uuid: string;
  uuid_short: string;
  name: string;
  description?: string;
  owner_id: number;
  node_id: number;
  egg_id: number;
  docker_image: string;
  startup_command: string;
  environment: Record<string, string>;

  memory_mb: number;
  swap_mb: number;
  disk_mb: number;
  io_weight: number;
  cpu_percent?: number | null;
  threads_pinned?: string;

  allocation_limit: number;
  database_limit: number;
  backup_limit: number;

  status: ServerStatus;
  container_id?: string | null;
  is_suspended: boolean;

  created_at: string;
  updated_at: string;

  live?: ResourceStats;
  node_name?: string;
  primary_address?: string;
}

export interface ResourceStats {
  server_uuid: string;
  cpu_percent: number;
  memory_bytes: number;
  disk_bytes: number;
  network_rx: number;
  network_tx: number;
  uptime_seconds: number;
  state: ServerStatus;
}

export type PowerAction = 'start' | 'stop' | 'restart' | 'kill';

export interface Node {
  id: number;
  name: string;
  fqdn: string;
  scheme: string;
  daemon_port: number;
  memory_mb: number;
  disk_mb: number;
  maintenance_mode: boolean;
  last_seen_at: string | null;
}

export interface NodeStatus {
  online: boolean;
  error?: string;
}

export interface CreateNodeRequest {
  name: string;
  location_id: number;
  fqdn: string;
  scheme?: string;
  daemon_port?: number;
  memory_mb: number;
  disk_mb: number;
}

export interface CreateNodeResponse {
  id: number;
  daemon_token: string;
}

export interface VersionInfo {
  version: string;
  commit: string;
  build_date: string;
  source_dir: string;
  repo_slug: string;
}

export interface UpdateCheck {
  current_version: string;
  latest_version: string;
  update_available: boolean;
}

export interface ActivityEntry {
  id: number;
  username: string | null;
  event: string;
  ip_address: string | null;
  created_at: string;
}

export interface EggVariable {
  name: string;
  env_variable: string;
  default_value: string;
  is_editable: boolean;
  rules: string;
}

export interface Egg {
  id: number;
  category: string;
  name: string;
  description: string;
  docker_image: string;
  startup_command: string;
  variables: EggVariable[];
}

export interface Allocation {
  id: number;
  node_id: number;
  ip: string;
  port: number;
  alias: string | null;
  server_id: number | null;
}

export interface CreateServerRequest {
  name: string;
  description?: string;
  node_id: number;
  egg_id: number;
  docker_image: string;
  startup_command: string;
  environment: Record<string, string>;
  memory_mb: number;
  swap_mb: number;
  disk_mb: number;
  allocation_id?: number;
}

export interface CreateAllocationRequest {
  node_id: number;
  ip: string;
  port: number;
  port_end?: number;
  alias?: string;
}

export interface ApiKey {
  id: number;
  name: string;
  permissions: string[];
  last_used_at: string | null;
  created_at: string;
}

export interface CreateApiKeyResponse {
  id: number;
  name: string;
  token: string;
}

export interface SSHKey {
  id: number;
  name: string;
  fingerprint: string;
  created_at: string;
}

export interface TwoFAStatus {
  enabled: boolean;
}

export interface TwoFASetup {
  secret: string;
  otpauth_url: string;
}

export interface FileEntry {
  name: string;
  is_directory: boolean;
  size_bytes: number;
  modified_at: number;
  mode: string;
}

export interface ScheduleTask {
  action: string;
  payload: string;
  time_offset_seconds: number;
}

export interface Schedule {
  id: number;
  name: string;
  cron_minute: string;
  cron_hour: string;
  cron_day_of_week: string;
  cron_day_of_month: string;
  is_active: boolean;
  only_when_online: boolean;
  last_run_at: string | null;
  tasks: ScheduleTask[];
}

export interface Subuser {
  id: number;
  user_id: number;
  email: string;
  permissions: string[];
}

export const SUBUSER_PERMISSIONS: { code: string; label: string }[] = [
  { code: 'control.start', label: 'Start' },
  { code: 'control.stop', label: 'Stop' },
  { code: 'control.restart', label: 'Restart' },
  { code: 'control.kill', label: 'Kill' },
  { code: 'console', label: 'Console' },
  { code: 'files.read', label: 'View files' },
  { code: 'files.write', label: 'Manage files' },
  { code: 'schedules.read', label: 'View schedules' },
  { code: 'schedules.write', label: 'Manage schedules' },
  { code: 'databases.read', label: 'View databases' },
  { code: 'databases.write', label: 'Manage databases' },
  { code: 'domains.read', label: 'View domains' },
  { code: 'domains.write', label: 'Manage domains' },
  { code: 'backups.read', label: 'View backups' },
  { code: 'backups.write', label: 'Manage backups' },
];

export const API_KEY_PERMISSIONS: { code: string; label: string }[] = [
  { code: 'servers.read', label: 'List/view servers' },
  { code: 'servers.write', label: 'Create/delete servers' },
  ...SUBUSER_PERMISSIONS,
];

export interface DatabaseHost {
  id: number;
  name: string;
  host: string;
  port: number;
  admin_username: string;
}

export interface CreateDatabaseHostRequest {
  name: string;
  host: string;
  port?: number;
  admin_username: string;
  admin_password: string;
}

export interface ServerDatabase {
  id: number;
  database_name: string;
  username: string;
  password: string;
  host: string;
  port: number;
}

export interface ServerDomain {
  id: number;
  domain: string;
  tls_status: string;
  created_at: string;
}

export interface ServerBackup {
  id: number;
  uuid: string;
  name: string;
  ignored_files: string[];
  bytes: number;
  checksum?: string;
  is_successful: boolean;
  completed_at: string | null;
  created_at: string;
}

export interface CreateScheduleRequest {
  name: string;
  cron_minute: string;
  cron_hour: string;
  cron_day_of_week: string;
  cron_day_of_month: string;
  only_when_online: boolean;
  tasks: ScheduleTask[];
}

export interface PanelUser {
  id: number;
  uuid: string;
  email: string;
  username: string;
  is_admin: boolean;
  totp_enabled: boolean;
  is_active: boolean;
  server_limit: number | null;
  last_login_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface UpdateUserRequest {
  is_admin: boolean;
  is_active: boolean;
  server_limit: number | null;
}
