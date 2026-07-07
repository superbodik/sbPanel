# add.md — design notes and roadmap (for me to keep improving this project)

This file is not user documentation — it's my own working notes so that the
next time I touch this repo, I pick up the same design language instead of
reinventing it. Update this file whenever a new page/pattern is added or a
plan changes; treat it as append/edit, not write-once.

## Current state (as of this writing)

**The product has a name now: Roost.** Picked to fit the bird/flight
vocabulary the codebase already committed to (`wingsd` the daemon, "eggs"
for server templates) without directly copying Pterodactyl's own naming —
servers "roost" on nodes, eggs hatch there. Applied everywhere a human
actually sees it: the browser tab title, the topbar/login logo (`name`
"Roost" + `tag`/`sub` "Panel" — same two-part logo shape panel.css already
had, just new words in the same slots), the installer's menu title and a
one-line `print_banner` shown before language selection, and the README's
H1. **Deliberately not renamed**: the Go module path
(`github.com/yourorg/panel`), systemd unit names (`panel.service`,
`wingsd.service`), env var prefixes (`PANEL_*`, `WINGSD_*`), the npm
package name, and `PANEL_REPO_URL`/`CLONE_DIR` in `install.sh` — those are
internal plumbing nobody but a developer reads, and renaming them is a
mechanical, error-prone, all-or-nothing refactor across every script and
import statement for zero user-facing benefit. If a full rename is ever
wanted, do it as its own dedicated pass, not bundled with a "let's also
add a feature" commit.

**2FA is real now** — the `.twofa-card` CSS and `users.totp_secret`/
`totp_enabled` columns had existed since the original scaffold but nothing
ever read or wrote them. Added `github.com/pquerna/otp` (verified with a
throwaway roundtrip test — generate a secret, compute the current code,
confirm it validates and a wrong one doesn't — before wiring it into any
handler, same standard as `files.SafeJoin`'s manual verification). Flow:
`POST /account/2fa/setup` generates a secret + `otpauth://` URL and stores
the secret **encrypted** (AES-GCM via `internal/crypto`, same treatment
as the daemon token per convention 8 — the panel needs to read it back on
every future login, not just verify it once) with `totp_enabled` left
`false`; `POST /account/2fa/verify` checks a code against that pending
secret and only then flips `totp_enabled` to `true`; `POST
/account/2fa/disable` requires the account password (not just being
logged in) before clearing it. `AuthHandler.Login` now takes an optional
`totp_code`: if the user has 2FA enabled and no code was sent, it responds
**428 Precondition Required** (not 401 — this is "give me one more thing,"
not "wrong credentials") with no body detail; the frontend's `login()`
special-cases exactly that status into a `TOTPRequiredError` so `Login.tsx`
can reveal the code field and resubmit, matching the same "bypass the
generic `request()` helper for a call with special status-code handling"
pattern `tryRefresh()` already used. A wrong TOTP code (as opposed to a
missing one) folds into the same generic "invalid credentials" 401
everything else uses — deliberately not distinguished from a wrong
password, so a failed login attempt can't be used to probe which factor
was wrong. No QR-code image rendering (`panel.css`'s `.twofa-card` never
had a slot for one) — the setup screen shows the `otpauth://` URL and raw
secret as copyable rows, same `.api-item`/`.api-key`/"Copy" pattern
already used for daemon tokens and API keys, so a user can paste the URL
into any authenticator app that accepts manual entry or add the secret by
hand.

Pages wired into `App.tsx`'s sidebar: **Servers** (dashboard,
`pages/Dashboard.tsx` + `components/ServerList.tsx`, now with a
**"+ Create Server" form** — `components/CreateServerForm.tsx` — that
actually creates a container on the daemon), **Nodes** (`pages/Nodes.tsx`,
now also manages **Allocations** per node), **Activity**
(`pages/Activity.tsx` — real log, not a placeholder), **Settings**
(`pages/Settings.tsx` — version + update check), **Account**
(`pages/Account.tsx` — API key management; reachable from the sidebar or
by clicking the user chip in the topbar). Clicking "Manage" on a server
card goes to **`pages/ServerView.tsx`**, tab-bar with
Overview/Console/Files/Databases/Schedules — Overview (power buttons +
live CPU/RAM/Disk meters), Console (real, bidirectional), Files
(`components/FileManager.tsx` — browse/edit/delete/create-folder, real
filesystem operations on the node), and now **Schedules**
(`components/ScheduleManager.tsx` — a real cron runner fires power
actions on a schedule) are wired; Databases is the one remaining honest
"not implemented yet" panel.

Backend REST surface: auth (login/me), nodes (list/create, admin-gated),
servers (list/get/power — power is now genuinely wired end to end, see
below), version (get/check-update). Two WS gateways, both now require
`?token=<jwt>` on the handshake (closed a real gap — they were previously
unauthenticated entirely, since browsers can't set an `Authorization`
header on a WS upgrade and nothing had filled that in yet): `/ws/servers/{uuid}`
relays live stats (polls the daemon every 2s while at least one browser is
subscribed), `/ws/servers/{uuid}/console` relays the daemon's console
bidirectionally (dials `wingsd`'s own `/ws/servers/{uuid}` on first
subscriber, closes it when the last browser leaves — same lazy pattern as
stats, see convention 10 below, now generalized to two independent
room/session maps in the same `Hub`).

**Power actions and live stats are real now, not stubbed.** The chain that
makes this work: `NodeHandler.Create` encrypts the raw daemon token
(AES-256-GCM, key = SHA-256 of `PANEL_ENCRYPTION_KEY`, see
`internal/crypto/aesgcm.go`) into `nodes.daemon_token_encrypted` alongside
the existing bcrypt hash (hash stays for future daemon-side auth
verification if that direction is ever added; encrypted copy is what lets
the *panel* re-authenticate outbound to wingsd). `cmd/panel/main.go`'s
`nodeClientResolver` decrypts it per-call and builds a real
`daemonclient.Client`. `daemon/internal/api/handlers.go` grew a
`GET /servers/{uuid}/stats` endpoint (CPU % computed from the standard
cpu-delta/system-delta/online-cpus formula, docker one-shot stats mode).
`ws.Hub` grew a lazy per-server poller (`FetchStats` callback + a
`pollers map[uuid.UUID]context.CancelFunc`) instead of a global ticker
over every server, so idle servers cost nothing.

**Migrations are now a real numbered system**, not a single re-run file.
`backend/migrations/000N_*.sql`, tracked in a `schema_migrations` table,
applied via `scripts/database.sh`'s `apply_migrations` — called both from
fresh installs (`provision_database`) and from `write_panel_env`/`run_update`
on existing installs (previously, `write_panel_env` returned immediately
if `panel.env` already existed, which meant **schema changes silently
never reached already-installed panels** — caught this while adding
migration `0002`, backfilled a bootstrap check so existing databases with
the old single-file schema and no `schema_migrations` row get `0001`
marked as already-applied instead of the migration runner trying to
`CREATE TABLE users` again and failing).

Installer (`install.sh` + `scripts/*.sh`): language select (EN/RU with
real explanations, not just translated strings), Docker/Postgres/Redis
provisioning, domain + Let's Encrypt, interactive admin bootstrap, node
install with a non-interactive fast path (`WINGSD_DAEMON_TOKEN=... bash
<(curl ...)`), update mechanism (`PANEL_UPDATE=1 ./install.sh`, now also
runs `apply_migrations`), full destructive uninstall gated behind typing
`DELETE`.

**Login now survives longer than 15 minutes.** `auth.TokenManager` issues
two JWTs at login: a short-lived access token (`AccessTokenTTL`, unchanged
at 15m) and a long-lived refresh token (`RefreshTokenTTL`, 30 days — the
config field existed since the original scaffold but was never actually
issued or consumed until now). Both carry a `type` claim (`"access"` |
`"refresh"`); `auth.Middleware` and `authenticateWS` both now reject
anything that isn't `type=access`, so a leaked/long-lived refresh token
can't be used to hit the API directly — it can only be exchanged at
`POST /auth/refresh` for a fresh access+refresh pair. Frontend:
`api/client.ts`'s `request()` catches a 401, calls `/auth/refresh` once
(de-duplicated across concurrent callers via a module-level
`refreshInFlight` promise so five simultaneous 401s don't fire five
refresh calls), retries the original request, and only forces a real
logout if the refresh itself fails (refresh token expired or revoked).

**The create-server flow exists now — the gap called out below as "the
single biggest gap" is closed**, at least for a first working version:
`backend/migrations/0003_seed_eggs.sql` seeds two starter eggs (a generic
Ubuntu container and `itzg/minecraft-server`); `EggHandler`/
`AllocationHandler`/`ServerHandler.Create` are new; the whole creation
happens inside one DB transaction that only commits *after* the daemon
confirms the container was actually created (rolls back the server row
and any claimed allocation otherwise, so a daemon failure never leaves a
ghost server behind). Along the way, fixed a real bug in
`daemon/internal/docker/manager.go`: container creation always overrode
`Cmd` with `/bin/sh -c "<startup_command>"`, even when empty — which
silently broke any egg (like `itzg/minecraft-server`) that relies on the
image's own `ENTRYPOINT`. Empty `StartupCommand` now leaves `Cmd` nil so
Docker falls through to the image's default. Allocations have no
provisioning UI beyond a bare-bones "add one manually" form on the Nodes
page (IP + port) — there's still no concept of a port range, reservation,
or auto-assignment; see roadmap.

**Activity logging** (`internal/activity`) is now called at the three
places that mattered most: login, node creation, server power actions,
and server creation. It intentionally swallows its own errors (logs to
stderr, never fails the calling request) — an activity-log write failing
is not a reason to fail the user's actual action.

**API keys are now a real auth method, not just CRUD.** `auth.Middleware`
takes a second argument, `auth.APIKeyResolver` — if the bearer token has
the `panel_` prefix (how raw API keys are formatted, see
`APIKeyHandler.Create`), it's sha256-hashed and looked up in `api_keys`
instead of being parsed as a JWT; a hit updates `last_used_at` and
produces the same `*auth.Claims` shape a JWT would (with
`Type: auth.TokenAccess`, so it passes the same access-only check).
WS auth (`authenticateWS`) deliberately was **not** extended to accept
API keys — those are for programmatic REST clients, not the browser SPA,
and `tm.Parse` on a `panel_...` string just fails as not-a-JWT, which is
the correct behavior there.

**Files tab does real filesystem operations on the node**, not a mock.
New daemon package `internal/files` operates directly on the host bind-mount
directory (`docker.Manager.ServerVolumePath`, now exported) rather than
exec'ing into the container — same approach Wings itself uses, and it
means files are readable/writable even while the container is stopped.
`files.SafeJoin` is the one function every operation goes through: it
`Clean`s the requested path as if rooted at `/`, joins it under the
server's data directory, then double-checks via `filepath.Rel` that the
result didn't escape via `..` — verified against `../../../etc/passwd`-
style inputs before wiring it up (see convention 15 below). `Delete`/
`Write`/`Rename` also explicitly refuse to touch the server root itself.
Wire-through: daemon HTTP endpoints (`/servers/{uuid}/files*`) → new
`daemonclient` methods (`ListFiles`/`ReadFile`/`WriteFile`/`DeleteFile`/
`CreateDirectory`/`RenameFile`) → backend `FileHandler` (owner-or-admin
check, same pattern as `ServerHandler.Delete`) → frontend
`FileManager.tsx` (breadcrumb path nav + `.files-table` listing + a plain
`<textarea>` editor — no syntax highlighting, this isn't a code editor,
just enough to fix a config file without SSH). `CreateContainer` now also
`os.MkdirAll`s the server's volume path before creating the container,
since it needs to exist for the Files tab to have something to list even
before the first daemon-side install step runs.

**Files tab now also has rename, upload, and download from the UI** —
the backend/daemon `RenameFile` plumbing already existed end to end, it
just needed a button (`handleRename`, `window.prompt` for the new name).
Upload/download needed one addition: `api/client.ts` grew `uploadFile`
(same binary-safe `PUT .../files/contents` endpoint the text editor's
`writeFile` already used, just with `Content-Type: application/octet-stream`
and a raw `File` body instead of a string) and `downloadFile` (a new
`requestBlob` helper alongside `request`/`requestText`, since `fetch`'s
`.blob()` is what lets an arbitrary-content response become a
browser-triggered file download via `URL.createObjectURL`) — no backend
or daemon changes needed, the existing endpoint was already binary-safe
in both directions. Binary files are now detected before being loaded
into the `<textarea>` editor (`looksBinary` — a NUL byte or a Unicode
replacement character in the decoded text is treated as "not text",
matching the daemon/backend's existing string wire format for file
contents) and the UI tells the user to use Download instead of silently
showing garbage.

**Schedules tab has a real cron runner behind it, not just CRUD.**
`internal/scheduler.Run` (launched via `go scheduler.Run(...)` in
`cmd/panel/main.go`) ticks every 60s, matches `server_schedules` rows
against `time.Now().UTC()` (deliberately UTC, not server-local time — the
DB column is `TIMESTAMPTZ` and the whole point is not caring what
timezone the panel host thinks it's in), and dispatches due schedules'
`schedule_tasks` in `sequence_id` order via the same `resolveNodeClient`
→ `daemonclient.Power` path everything else uses. Two things worth not
re-deriving next time:
- **The cron matcher is deliberately simplified** — `cronFieldMatches`
  only understands `*` (any) or one exact integer; no comma lists
  (`1,15`), no ranges (`1-5`), no step values (`*/5`). Verified by hand
  against a handful of cases before wiring it up. Good enough for "every
  night at 3am" / "every Sunday", not good enough for a general-purpose
  cron replacement — extend `cronFieldMatches` itself if that's ever
  needed, don't add a second matcher.
- **Firing is claimed atomically** via
  `UPDATE server_schedules SET last_run_at = $1 WHERE id = $2 AND
  (last_run_at IS NULL OR last_run_at < $1)`, checking
  `RowsAffected() == 0` before dispatching. This is what stops the same
  schedule firing twice if the ticker's tick and a slow query ever
  overlap — don't dispatch a task before this claim succeeds.
**`command` tasks are real now too** — `scheduler.execute()`'s `command`
branch calls the new `daemonclient.Client.SendCommand`, which hits a new
daemon endpoint (`POST /servers/{uuid}/command`). The daemon side reuses
`console.Hub`'s existing (previously write-only-to-itself) `writers` map:
if a browser already has the console WS open, the scheduled command
writes to that same stdin pipe; if nobody's watching, `Hub.SendCommand`
opens its own brief `docker.Attach`, writes one line, and closes it —
same attach/detach lifecycle `Hub.Serve` already used for real sessions,
just without a WS wrapped around it. This is what makes a scheduled
command reliable even when no one has the console tab open, which was
the whole point of it being scheduled. `ScheduleManager.tsx` grew a
"Task type" selector (Power action / Console command) so this is
reachable from the UI, not just the API. `backup` is still a no-op —
that needs actual backup infrastructure that doesn't exist yet (same
`database_hosts`-style "out of scope v1" as the Databases tab).

**Server deletion is wired end to end**: `ServerHandler.Delete` checks
owner-or-admin, calls the daemon's `DeleteServer`, then deletes the row
(FKs cascade/null out `server_subusers`/`server_variables`/`allocations.
server_id` automatically). Supports `?force=true` for admins to drop the
DB record even if the node/daemon is unreachable (e.g. a decommissioned
node) — without it, a daemon-side failure blocks the delete rather than
silently orphaning a running container. Frontend: a `.danger-card` in
ServerView's Overview tab, `window.confirm` before calling it (no custom
modal system exists yet, and this is a one-off enough action that
building one wasn't justified).

**RBAC is real now — subusers can be granted per-server access, and two
previously-real "any authenticated user can touch any server" holes got
closed along the way.** `auth.SubuserChecker` implements the
`PermissionChecker` interface that had sat unused since the original
scaffold; it reads `server_subusers.permissions` (a JSONB array of
strings — confirmed by reading `pgx/v5@v5.5.4`'s own `pgtype/json.go`
that scanning `jsonb` into `*[]byte` copies the raw JSON bytes verbatim,
since there was no local Postgres available with this project's schema
to round-trip test against directly). Permission codes:
`control.start`/`stop`/`restart`/`kill`, `console`, `files.read`/`write`,
`schedules.read`/`write` — granular enough to say "this collaborator can
restart the server and read the console but not touch files," coarse
enough that nobody has to think about it as a real ACL system.
`ServerHandler.List` now `LEFT JOIN`s `server_subusers` so a subuser
actually sees servers they've been added to (previously: not at all,
despite the table existing since day one). New `SubuserHandler`
(`GET`/`POST`/`PATCH`/`DELETE` on `/servers/{uuid}/subusers`) is
owner-or-admin only — a subuser can never grant themselves or anyone else
more access, only the server's actual owner or a site admin manages the
list. Frontend: a new "Sharing" tab in `ServerView` (`SubuserManager.tsx`)
— add a collaborator by email, toggle permissions with the same
`toggle-sw` switch `ScheduleManager` already uses. Following this
project's established pattern of never gating UI on a client-known role
(nothing here tracks `is_admin` in the frontend at all — Nodes/Allocations
pages have always just let the backend 403 non-admins), the Sharing tab
doesn't try to detect "am I the owner" client-side either: it calls
`GET .../subusers`, and if that 403s (meaning the viewer isn't the owner
or an admin), it shows "only the owner or an admin can manage sharing"
instead of a broken list.

**Two real, pre-existing "any authenticated user can touch any other
user's server" bugs got found and fixed while wiring this up — not
hypothetical, actually reachable:**
1. `ServerHandler.Get` (`GET /servers/{uuid}`) and `ServerHandler.Power`
   (`POST /servers/{uuid}/power`) had **no ownership check at all** —
   any logged-in user could view any other user's server details, and,
   worse, start/stop/restart/kill any other user's server just by
   knowing (or guessing/enumerating) its UUID. Both now require
   admin/owner/subuser-with-permission via `SubuserChecker.CanAccessServer`
   (mapped per power action: `start`→`control.start`, etc.) or
   `CanViewServer` (any subuser membership, for read-only `Get`).
2. Both WS endpoints (`/ws/servers/{uuid}` stats, `/ws/servers/{uuid}/console`)
   validated the JWT itself but never checked *which* server the token
   holder was allowed to see — `authenticateWS` only proved "this is a
   valid access token for *some* user," not "for *this* server." Any
   authenticated user could subscribe to any other server's live stats or,
   far more seriously, open its console and send arbitrary commands. Fixed
   via a new `canAccessServerWS` check (stats needs `CanViewServer`,
   console needs `CanAccessServer(..., auth.PermConsole)`), run right after
   parsing the UUID and before `authenticateWS`'s claims are trusted for
   anything server-specific.
Both were found by grep'ing every handler for a `claims.IsAdmin`/`ownerID`
check and noticing which ones were missing one entirely, not by a
targeted audit — the same "grep for `r.Get`/`r.Post` registered outside
the auth group" instinct from the earlier WS-auth gap, generalized to
"grep for handlers that read `owner_id` but never compare it to anything."
Worth doing again the next time a new server-scoped handler is added.

**API keys are scoped now too, using the same permission codes as
subusers.** `auth.Claims` grew a `KeyPermissions *[]string` field — `nil`
means "unrestricted" (every JWT session, and any API key created with an
empty permissions list, which is what all pre-existing keys already have
by default), a non-nil slice means "only these codes." `Claims.HasKeyPermission`
is the single check; every server-scoped handler now does
`claims.HasKeyPermission(permission) && Subusers.CanAccessServer(...)` —
two independent conditions, both must pass. This matters for a subtlety
that's easy to get backwards: `CanAccessServer`/`CanViewServer` short-
circuit true for `claims.IsAdmin`, so an *admin's* scoped API key still
needs `HasKeyPermission` checked as a separate condition, not folded into
the same call — otherwise an admin's "read-only" key would silently
regain full access purely from being an admin, defeating the entire
point of scoping it down. Reused the exact same permission codes
(`control.*`, `console`, `files.*`, `schedules.*`) plus two new
account-wide ones for actions that aren't tied to one server yet at
request time — `servers.read` (list/get) and `servers.write`
(create/delete). Frontend: Account page's "New key name" form grew the
same `toggle-sw` permission picker `SubuserManager` uses, with "leave
everything unchecked for full access" as the explicit default explained
in the UI (matches the empty-array-means-unrestricted backend
convention, so it doesn't need its own migration for existing keys).

**There's a Users admin page now — there wasn't one at all before, for
anyone.** Went looking for where to add a per-user server-creation quota
and found there was no way for an admin to list users, deactivate one, or
change anyone's admin status short of hand-editing the database — the
only user-management moment in the entire product was the one-time admin
bootstrap during install. New `UserHandler` (`GET /users`, `PATCH
/users/{id}`, both `auth.RequireAdmin`-gated) and a `Users` sidebar page
(`.db-table`, matching Nodes/Allocations) with a toggle for admin status,
a toggle for active/deactivated, and a `server_limit` field (new nullable
`users.server_limit INT` column, migration `0004`; `NULL` = unlimited,
matches how `nodes.last_seen_at`/other nullable columns already read as
"nothing set yet = no restriction" elsewhere in this schema).
`ServerHandler.Create` now checks it (skipped for admins) — count the
user's existing servers, compare to `server_limit`, 403 if at or over.
**Update semantics are full-replace, not partial-patch, on purpose**:
`PATCH /users/{id}` always requires `is_admin`/`is_active` and always
takes whatever `server_limit` value is sent (including explicit `null`)
as the new value — no "send only the field you're changing" merge logic.
This sidesteps a real ambiguity: Go's `encoding/json` can't distinguish
"key omitted" from "key explicitly null" when decoding into `*int`, so a
partial-update design would have had no way to tell "leave server_limit
alone" from "clear it back to unlimited." Full-replace works because the
frontend's per-row form always holds and submits the complete current
state of all three fields together — there's no scenario where only one
field is known and the others need to be preserved from the server.
Guard rail: an admin can't remove their own admin status or deactivate
themselves through this endpoint (`id == claims.UserID` check) — the
only way that risk existed already (unwitting self-lockout via a raw SQL
edit) shouldn't become a one-click UI mistake now that this is exposed.

**Nodes now have a real connectivity check, not just a `last_seen_at`
column that nothing ever writes to** — the Status column used to show
"Online"/"Never seen" based on `nodes.last_seen_at`, but no code path
anywhere ever updates that column, so it silently always said "Never
seen" regardless of actual state (found while chasing a live "POST
/servers failed: 502" report — the Nodes page gave no way to tell
"daemon down" from "bad token" from "wrong port" without SSHing in and
reading `journalctl -u panel`). Added a real fix: `wingsd` grew an
unauthenticated `GET /healthz` (moved `RequireDaemonToken` into an
`r.Group` so it doesn't cover this one route — a health check has to
work *before* proving you have a valid token, that's the whole point),
`daemonclient.Client.Ping` hits it, and the panel exposes
`GET /nodes/{id}/status` (resolves the node's client the same way
server-create does, so it surfaces the exact same failure modes: missing
`daemon_token_encrypted`, decrypt failure, connection refused, etc.) A
"⟳" button on the Nodes page calls it on demand and shows the real error
inline (`title` attribute has the full text, the row shows a truncated
one-liner) — this turns "why did create fail" from "read the server's
systemd journal" into "click one button in the UI."

**The frontend was discarding every backend error message — this is
almost certainly why the same "POST /servers failed: 502" kept coming
back no matter what got fixed server-side.** `api/client.ts`'s
`request`/`requestText`/`requestBlob` all threw a hardcoded
`` `${method} ${path} failed: ${status}` `` on any non-OK response and
never read the response body — so every specific message I'd carefully
written on the backend (`"node unavailable"`, `"daemon failed to create
server"`, the node-token-missing message, etc.) was being thrown away
before it ever reached the screen. Every backend fix to *what* the error
said was invisible to the user the whole time. Fixed with one shared
`errorMessage(res, path, init)` helper — reads `res.text()` (Go's
`http.Error` always sends plain text, never JSON, so this is safe
without a content-type check), appends the status code, falls back to
the old generic message only if the body is empty or unreadable. All
three request helpers call it now. **Lesson**: when a backend keeps
getting more specific error messages but a user keeps reporting the
same generic-looking failure, check whether the frontend is actually
capable of displaying anything more specific before assuming the
backend fix didn't work or guessing at new backend causes — the wiring
between the two is exactly as fixable as either end alone.

**Found the actual root cause of the live "POST /servers failed: 502"
reports, once the error message could finally reach the screen:
`wingsd` only speaks HTTPS if `WINGSD_TLS_CERT`/`WINGSD_TLS_KEY` are set
(`daemon/cmd/wingsd/main.go` falls back to plain HTTP with a log warning
otherwise), and `scripts/daemon.sh` never sets those up — every fresh
install runs the daemon in plain-HTTP fallback mode. But
`NodeHandler.Create` defaulted every new node's `scheme` to `"https"`
regardless, and the "Add node" form had no field to override it. Every
single node created through the normal install-and-click-Add-node flow
was silently misconfigured to speak HTTPS to a daemon that was only
ever listening on plain HTTP — a systemic default mismatch, not a
one-off misconfiguration. Fixed the default (now `"http"`, matching what
actually happens without extra TLS setup) and added a scheme selector to
the Add Node form so anyone who *has* configured `WINGSD_TLS_CERT/KEY`
can still pick `https`. Also added `PATCH /nodes/{id}` (admin-only, name/
fqdn/scheme/daemon_port/memory/disk — deliberately excludes the daemon
token, which doesn't need to change) so a node created with the wrong
scheme can be corrected in place from the same expanded panel the delete
button lives in, without regenerating its token or reinstalling `wingsd`
on the machine. This is the kind of default that's invisible until
someone hits it, and then invisible *again* until the frontend can show
what actually broke — worth remembering alongside the error-surfacing
fix above, since neither one alone would have made this findable.

**A quick security pass turned up two real vulnerabilities and one
piece of already-provisioned infrastructure that was sitting completely
unused.**
- `config.Load()` defaulted `PANEL_JWT_SECRET` to the literal string
  `"change-me-in-production"` and `PANEL_ENCRYPTION_KEY` to `""` if
  either env var wasn't set. The installer always generates real random
  values for both (`scripts/panel.sh`), so this fallback should never
  fire in the normal flow — but that string is now sitting in a *public*
  GitHub repo, and any install where the env var didn't get loaded
  (missing `panel.env`, a broken systemd `EnvironmentFile`, running the
  binary directly without it) would silently start up with a
  **publicly-known JWT-signing secret**, letting anyone forge an admin
  token. `config.Load()` now calls `log.Fatal` if either is unset or
  still equals the known-bad default — fail closed, not open. This is
  the same class of mistake as `install.sh`'s cwd-deletion bug earlier
  this session: a fallback that's "safe" in the intended path becomes
  a real hole the moment something upstream doesn't go as planned.
- **Redis was fully provisioned (installed, health-checked, written into
  `panel.env`) and never once connected to from Go code.** `config.go`
  had `RedisAddr`/`RedisPassword` fields that nothing read. Meanwhile
  `/auth/login` and `/account/2fa/verify` had zero rate limiting —
  unlimited password guesses and, for accounts with 2FA on, unlimited
  6-digit TOTP-code guesses. New `internal/ratelimit` package (`Limiter.Allow`,
  a plain Redis `INCR`+`EXPIRE` fixed-window counter) is now checked at
  the top of both: 10 attempts per 15 minutes, keyed by client IP for
  login and by user ID for 2FA verify. **Deliberately fails open**: if
  Redis is unreachable, the check logs the error and allows the request
  rather than locking everyone out of login because a secondary defense-
  in-depth layer is down — rate limiting is one layer, not the only one
  (bcrypt + TOTP still apply regardless). Known gap: this only throttles
  by IP, not by target account, so a distributed brute force against one
  specific victim's email from many IPs isn't caught — would need a
  second, account-keyed limiter to close that, not implemented yet.
- **Watch out when adding a Redis client dependency**: `go get
  github.com/redis/go-redis/v9@latest` silently bumped `go.mod`'s `go`
  directive from `1.22` to `1.24`, because the latest go-redis requires
  it — but `scripts/toolchain.sh` only provisions Go `1.22.5` on fresh
  installs, so that upgrade would have broken every install's build.
  Pinned to `v9.14.0` (last version compatible with `go 1.22`) and
  manually restored the `go` directive after `go mod tidy` tried to bump
  it again. Check `go.mod`'s `go` line after *any* `go get`/`go mod tidy`
  against what `scripts/toolchain.sh`'s `GO_VERSION` actually installs —
  a passing local build doesn't catch this since the local toolchain is
  usually newer than what's pinned for fresh installs. **This happened
  again, one layer deeper, adding `go-sql-driver/mysql` for the
  Databases feature**: the driver itself (`v1.8.1`) only declares `go
  1.18` and looked safe, but `go mod tidy` still bumped the directive to
  `1.24` — because it pulled in `filippo.io/edwards25519` at whatever
  latest version satisfied MVS, and *that* transitive dependency is what
  actually required `1.24`, not the driver I'd deliberately pinned.
  Pinning your *direct* dependency to an old-enough version isn't
  sufficient by itself — a transitive dependency can independently force
  the same bump. The reliable check is: after any dependency change,
  manually set the `go` directive back to what `toolchain.sh` installs
  and run a plain `go build ./...` (not `go mod tidy`, which will
  re-resolve and re-bump); if it still builds clean, every version in
  the resolved graph is actually compatible, regardless of what any
  individual package's own `go.mod` claims to require.

**A follow-up security pass on request/connection size limits found two
more real, if lower-severity, resource-exhaustion gaps.**
- `FileHandler.Write` called `io.ReadAll(r.Body)` with **no size limit
  at all** — any user holding `files.write` (which a subuser can be
  granted, not just the owner) could send an arbitrarily large upload
  and exhaust the *panel* process's memory. The daemon side was already
  safe (`files.Write` streams straight to disk via `io.Copy`, never
  buffers the whole thing), so this was panel-only. Rather than patch
  just this one handler, added a single global `maxBodySize` middleware
  (100 MiB via `http.MaxBytesReader`, applied via `r.Use()` before
  routing) covering every request on both the panel and `wingsd` — every
  JSON-body handler in the codebase had the same latent gap (unbounded
  `json.NewDecoder(r.Body).Decode`), not just this one, so a global fix
  was the right shape rather than a one-off per-handler patch. 100 MiB
  is generous for real config files while still bounding worst-case
  memory use; genuinely large files (logs, world saves) still need real
  streaming, which is the same known Files-tab gap already tracked in
  the roadmap below — this doesn't fix that, it just stops the unbounded
  case from being trivially exploitable in the meantime.
- Neither WS hub (`ws.Hub` on the panel, `console.Hub` on `wingsd`)
  called `conn.SetReadLimit(...)` after upgrading — gorilla/websocket
  defaults to no message-size cap at all, so an authenticated connection
  (anyone with `console` permission, not just admins) could send one
  oversized WS frame and force a large allocation. Added `SetReadLimit`
  to both: 4096 bytes on the stats socket (client-to-server messages are
  never actually read for anything, just drained to detect disconnect),
  32 KiB on both console sockets (generous for a single command line,
  small enough to matter as a cap).
- **Verified no sensitive column ever reaches a JSON response**: grepped
  every handler for `daemon_token_encrypted`/`totp_secret`/`password_hash`/
  `admin_password_encrypted` — every occurrence is either a write, or a
  read immediately followed by decrypt-and-use (never assigned into a
  struct that gets `writeJSON`'d back to the client). Worth re-running
  this same grep after adding any new encrypted-secret column, since
  it's a one-line check that catches an entire class of accidental-leak
  bug before it ships.

**2FA setup now actually shows a QR code, and there's real mobile/tablet
navigation — neither existed before, both found from a user screenshot,
not a report of "it's broken."**
- The 2FA setup screen only ever showed the raw `otpauth://` URL and
  secret as text — no image, despite the UI text literally saying "scan
  this with your authenticator app." Added `qrcode` (client-side only,
  generates a data URL from a canvas — no network call, no CDN, nothing
  server-side needs to change) and render it as an `<img>` above the
  copyable text rows, which stay for anyone who'd rather type the secret
  in by hand.
- **`panel.css` had exactly one breakpoint (900px), and its only mobile
  behavior was `.sidebar { display: none; }`** — with no hamburger, no
  drawer, nothing to bring navigation back. Under 900px wide, a user
  could never leave whatever page they landed on. Verified this was
  real (not just a code-reading guess) by actually launching the Vite
  dev server and screenshotting both a 375px and a 1280px viewport with
  Playwright before and after the fix — the `run` skill's guidance to
  drive the app rather than trust a clean build, applied literally,
  since a CSS/layout change is exactly the kind of thing `tsc`/`vite
  build` passing tells you nothing about. Added a `.mobile-nav-toggle`
  hamburger button (topbar, hidden above 900px via the same breakpoint),
  a slide-in `.sidebar.mobile-open` drawer (`transform: translateX`,
  solid `--bg-2` background instead of the desktop sidebar's translucent
  tint, since it now sits *over* page content instead of beside it), and
  a `.sidebar-backdrop` to close it on outside-tap. `goTo()` already
  centralized every nav click, so closing the drawer on navigate was a
  one-line addition there, not a per-nav-item change. Also added a
  520px breakpoint (topbar breadcrumb/username/separator hide, stat
  cards and the power-button grid reflow to 2 columns) since 900px alone
  left small-phone widths cramped.
- No project `run` skill existed for this repo yet (checked before
  falling back to the generic browser-driven pattern) — worth generating
  one via `/run-skill-generator` next time this comes up, so "start the
  dev server, screenshot at N viewports" doesn't need re-deriving
  (installing Playwright, finding the proxy config, etc.) from scratch
  again.

**Doing a route-by-route pass over `router.go` (every path, checked
against what it actually returns and who's allowed to call it) found
one real, serious information-disclosure bug**: `GET /activity` had no
authorization gate at all — every other admin-management route
(`/nodes` writes, `/users`, `/database-hosts` writes, `/allocations`
writes) uses `auth.RequireAdmin`, but this one was plain
`r.Get("/activity", ...)`. `ActivityHandler.List` itself has no
per-user filtering either — it returns the last 100 `activity_logs`
rows *panel-wide*, including `ip_address` and `username` for every
actor. Any authenticated non-admin user — a regular account that owns
zero servers — could see every other user's login IP addresses and
every action anyone had taken anywhere in the panel. The frontend's own
copy ("Recent actions across the panel") already signals this was
designed as an admin audit view, not a personal feed, so the fix is the
missing gate, not new filtering logic: added
`r.With(auth.RequireAdmin)`. `Activity.tsx` now shows "Only admins can
view the activity log" on a fetch failure instead of the raw error
text, matching the same blanket-treat-any-error-as-403 pattern
`SubuserManager.tsx`/`Users.tsx` already use for their own admin-gated
resources. **Worth re-running this specific check** (grep `router.go`
for every route lacking `auth.RequireAdmin`, then ask "should a
brand-new non-admin account be able to call this, and does the handler
filter results to that account if not") whenever a new list-style
endpoint is added — this is exactly the kind of gap `go build`/`go vet`
can never catch, since the code is entirely correct Go, just missing an
authorization decision.

**Found the same "if file exists, skip forever" trap as the migrations
incident, this time in `scripts/daemon.sh` — and it was actively
blocking a live user from fixing a real 401.** After the http/https
scheme fix, the user's node started returning `node daemon returned 401`
on every daemon call — the panel's stored token had drifted from
whatever `wingsd` actually had configured (exactly how is unclear, but
irrelevant once you notice there was **no way to fix it**: re-running
the install one-liner with a new `WINGSD_DAEMON_TOKEN` hit
`write_daemon_env`'s `if [[ -f "$DAEMON_ENV_FILE" ]]; then ... return;
fi` guard and silently did nothing. `install_daemon` also used `systemctl
enable --now`, which doesn't restart an already-running unit even if its
env file *did* change. Fixed both: `write_daemon_env` now updates just
the `WINGSD_DAEMON_TOKEN=` line via `sed` in place when the file exists
and a token is provided (leaving `WINGSD_NODE_UUID` and everything else
untouched), and `install_daemon` does `enable` + `restart` explicitly.
Paired with a new `POST /nodes/{id}/regenerate-token` (admin-only, same
`generateToken`/hash/encrypt pattern as node creation) and a
"Regenerate token" button in the Nodes page's expanded panel — reusing
the exact same copy-paste install-command UI node creation already
shows, since with the installer fix that command now actually works for
an *existing* install too, no full delete-and-recreate needed. **The
lesson from the migrations incident generalized**: any installer step
gated behind "if the file/record already exists, do nothing" needs a
second look at *which specific piece* should really be one-time
(the node UUID) versus updatable-on-demand (the token) — the guard was
protecting the wrong granularity.

**Found the real cause of a `"name, node_id, egg_id... are required"
(400)` report**: `CreateServerForm`'s Node/Egg `<select required>`
placeholder options used `value={0}`, which renders as the DOM string
`"0"` — not an empty string. HTML5's native `required` validation on a
`<select>` only blocks submission when the value is exactly `""`, so
`"0"` satisfies it silently. A user could click Create without ever
choosing a node or egg and the browser wouldn't stop them, sending
`node_id: 0, egg_id: 0` straight to a backend that correctly rejected
it — the 400 was the validation working, just one layer too late.
Fixed with an explicit check in `handleSubmit` before the API call,
rather than trying to coerce the placeholder into an empty-string value
(would need parallel string/number state juggling for no real benefit) —
matches how this codebase already prefers explicit JS validation with a
friendly `setError` message over relying on native constraint
validation elsewhere.

**Redesigned the 2FA step during login as an actual separate screen, not
an appended field, and added a real animation system across the app.**
`Login.tsx` used to render the TOTP code input *below* the email/password
fields once required — functionally fine, but not what "enter your 2FA
code" flows anywhere else look like (GitHub, Google, etc. all swap the
whole screen). Now `!needsTotp` renders one `<div className="login-step">`
with credentials, `needsTotp` renders a **different** one with just the
code field, a "Back" link, and its own heading — full replace, not
append, with a `stepIn` slide-fade transition on the swap. Also went
through and added a proper motion layer to `panel.css`: drifting ambient
background blobs (`blobDrift`, previously static), staggered fade-in for
grid/list items (server cards, table rows — `nth-child` delays capped at
6 items so a long list doesn't take visibly long to finish appearing),
spring-eased hover/press feedback on buttons and stat cards, a glow pulse
on the active nav item, and a scale/rotate flourish on the user avatar on
hover. Every new animation lives inside `@media
(prefers-reduced-motion: no-preference)`, with a matching `reduce` query
that collapses all animation/transition durations to near-zero — added
both at the same time, not as a follow-up, since shipping motion without
the accessibility opt-out from the start is the mistake to avoid, not a
polish item to add later. Verified all of this with real Playwright
screenshots (dev server + mocked localStorage auth to get past the login
gate without a backend) rather than trusting a clean `vite build` — a
CSS-only regression is invisible to any of the type-checking or build
tooling in this project.

**Once the frontend could actually display backend error text (previous
entry), the next problem was that some backend messages were still
deliberately vague.** `ServerHandler.Create`/`Power`'s "node unavailable"
and "daemon failed to create server" / "daemon rejected power action"
used to swallow the real underlying error into `log.Printf` only,
returning a fixed string to the client — a leftover from before the
frontend could show anything useful, when there was no point sending
detail nobody could see. Now that it can, these four sites append
`err.Error()` (or `daemonResp.Message` for a daemon-side rejection) to
the HTTP response body too. This is consistent with what
`NodeHandler.Status`'s "⟳" check already exposes (raw daemon-connectivity
errors) to the same audience — anyone who can create/power a server can
already see the same class of infrastructure detail via the Nodes page,
so this isn't a new exposure, just the same information reaching the
place a user actually hits the problem instead of requiring a detour
through the Nodes page or `journalctl`.

**`models.Server` never actually had `node_name`/`primary_address` —
the frontend has been ready for them since `ServerCard.tsx`/`ServerView.tsx`
were first written, they just silently never rendered.** Found this
doing a review pass, not chasing a specific report: `types/index.ts`'s
`Server` interface already had `node_name?`/`primary_address?` and
`ServerCard.tsx` already had `{server.node_name && ...}` conditionals
and `panel.css` already had `.srv-node`/`.srv-addr` styling for them —
but `ServerHandler.List`/`Get`'s SQL never selected anything to fill
those fields, and the Go `models.Server` struct didn't even have them.
The dashboard has apparently never shown which node a server lives on or
its connection IP:port, since the very first version of this page.
Fixed by joining `nodes` (for the name) and a per-server correlated
subquery against `allocations` (first one by id, ordered — there's no
"primary allocation" flag in the schema, and in practice a server has at
most one allocation today since `ServerHandler.Create` only ever binds
the single `allocation_id` sent at creation time, so "first by id" and
"the only one" are the same thing right now). Also surfaced the same two
fields in `ServerView.tsx`'s Overview tab, which hadn't shown them
either. If multi-allocation-per-server ever becomes a real feature,
revisit this "first by id = primary" shortcut with an actual flag.

**Nodes can be deleted now, and egg variables actually reach the
container.** Two small gaps closed together: `DELETE /nodes/{id}`
(admin-only) refuses if any server still references that node (checked
explicitly for a clean 409, since `servers.node_id`'s FK has no `ON
DELETE` clause and would otherwise surface a raw constraint-violation
error) — `allocations.node_id` cascades on delete so those clean up
automatically. Frontend: clicking a node's name (not a separate button)
toggles an inline expanded panel with the delete action, since Nodes had
no per-node detail/management surface at all before this.

Separately — while adding three new Python eggs (see below) — found that
`CreateServerForm.tsx` always sent `environment: {}` on server creation,
no matter what. This meant even the *original* Minecraft egg's own
description ("Set EULA=TRUE in environment") was already impossible to
follow from the UI; there was simply no field for it. `EggHandler.List`
now includes each egg's `egg_variables` rows (name, env var, default,
editable, rules) in its response; `CreateServerForm.selectEgg` seeds a
new `environment` state from those defaults, renders one input per
variable, and actually submits it. Confirmed end to end: `Server.Environment`
was already wired all the way through the daemon (`docker.Manager`
turns the map into real container `Env` entries) — this was purely a
frontend gap, nothing on the backend/daemon side needed to change.

**Three new eggs**, all `python:3.12-slim` with a `pip install -r
requirements.txt` (best-effort, errors suppressed since not every
project has one) followed by `python3 $START_FILE` — "Python: Website",
"Python: Telegram Bot", "Python: Discord Bot". Deliberately unopinionated
about which web framework or bot library — this just runs whatever
entrypoint file the user uploads via the Files tab, same
upload-your-own-code shape as the "Custom Docker Container" egg, just
pre-filled with a sensible Python base image and a dependency-install
step. `START_FILE` variable defaults to `app.py` for the website egg,
`bot.py` for both bot eggs — editable per-server now that egg variables
actually flow into the create form.

## Design conventions — follow these before inventing new patterns

1. **Never invent new CSS.** `frontend/src/styles/panel.css` is the design
   system; it was handed to me finished and I don't touch it. Every new page
   is built by finding the closest existing section in panel.css and reusing
   its classes verbatim, even if the semantic match is a little loose (e.g.
   the Nodes table reuses `.db-table/.db-head/.db-row` — "database list"
   markup — because it's the right shape of table, not because nodes are
   databases). If a new page truly needs a shape nothing in panel.css
   covers, that's the one exception where adding a small amount of new CSS
   in a *separate* file is acceptable — but check twice first.
2. **Copyable-command pattern**, established in Nodes/Settings: when an
   action needs to happen on a *different* machine than the browser (install
   a node, run an update), the UI's job is to show a single copy-pastable
   shell command (`.api-item` + `.api-key` + a `.btn-sm` "Copy" button using
   `navigator.clipboard`), not to try to execute it remotely. The panel
   process runs as `www-data` with no privilege to restart its own systemd
   unit or touch Docker on other hosts — don't build a "click to apply"
   button that secretly shells out; it can't have the permissions to do that
   safely, and giving it those permissions is a bigger security decision
   than a UI ticket.
3. **New sidebar page checklist** (do all four or the page is orphaned):
   add the `View` union type in `App.tsx`, add the `nav-item` with
   `onClick={() => goTo('x')}` and the `active` class ternary, add the
   render branch in `<main>`, and reset `activeServer` in `goTo()` if the
   page has no concept of a "current server" (copy the existing pattern,
   don't rewrite it).
4. **i18n pattern** (installer only, not the frontend yet): all
   user-facing *explanatory* strings go through `scripts/i18n.sh`'s
   `MSG_EN`/`MSG_RU` tables and `msg()`. Plumbing/log lines (`log_ok`,
   `log_step`) stay English-only — i18n is for the handful of steps where a
   Russian operator genuinely needs the "why", not for every log line.
5. **Non-interactive fast paths**: every interactive installer prompt should
   have an env-var bypass (`WINGSD_DAEMON_TOKEN`, `PANEL_UPDATE`) so the
   website can eventually generate a single copy-paste command for it. When
   adding a new interactive step, ask "would a copy-paste command from the
   website want to skip this?" — if yes, add the env var check at the same
   time, not as a follow-up.
6. **No comments in code.** This was an explicit, deliberate instruction
   from the project owner, applied retroactively across the whole codebase
   (Go, TS, SQL, proto, bash). Keep following it for new code: no `//`, `#`,
   `--`, or `/* */` explanatory comments anywhere in source files. This
   file and other `.md` docs are the exception — comments belong in design
   notes, not in code.
7. **Build/version discipline**: `scripts/panel.sh`'s `build_panel_binaries`
   embeds `commit`/`buildDate` via `-ldflags -X main.commit=... -X
   main.buildDate=...` into `cmd/panel`. If another binary ever needs to
   report its own version (wingsd, for instance), wire it the same way
   rather than inventing a second mechanism.
8. **Secrets that the panel needs to use later (not just verify) get
   encrypted, not hashed.** `daemon_token_hash` (bcrypt) proves someone
   presented the right token; it can never answer "what was the token" —
   that's what `daemon_token_encrypted` (AES-GCM via `internal/crypto`,
   keyed off `PANEL_ENCRYPTION_KEY`) is for. Before adding another secret
   column, ask which of these two questions the code actually needs
   answered later.
9. **Any new SQL schema change is a new numbered file in
   `backend/migrations/`, never an edit to `0001_init.sql`.** The migration
   runner tracks applied files in `schema_migrations`; editing an old file
   in place means already-deployed panels never see the change (they've
   already recorded that filename as applied). Bump the number instead.
10. **Live per-server data (stats, console) is relayed lazily, per room.**
    `ws.Hub` only starts polling/dialing for a given server once the first
    browser subscribes to its room, and tears the upstream connection down
    the moment the last one disconnects — `pollers`/`consoleSessions`
    (both `map[uuid.UUID]context.CancelFunc`-shaped) in `internal/ws/hub.go`.
    Don't build a global "poll/dial every server all the time" loop — most
    servers have nobody watching at any given moment. When adding a third
    kind of live relay, add a third independent room+session map rather
    than trying to generalize the first two into one abstraction
    prematurely — stats (HTTP poll) and console (persistent WS dial) have
    different enough connection lifecycles that forcing one interface over
    both cost more than the duplication would.
11. **WS auth is a `?token=<jwt>` query param, not a header.** Browsers
    cannot set `Authorization` on a WebSocket upgrade request, so
    `auth.Middleware` (header-based) doesn't apply to `/ws/*` routes — they
    validate the query param directly (`authenticateWS` in `api/router.go`)
    instead of being wrapped in the same middleware group as the REST API.
    Every new `/ws/*` route needs this same explicit check; it will not be
    caught by putting the route inside `r.Group(auth.Middleware(...))`.
12. **Two-token auth: access is short-lived and API-usable, refresh is
    long-lived and single-purpose.** A JWT's `type` claim (`auth.TokenType`)
    is what enforces this — `auth.Middleware`/`authenticateWS` both check
    `claims.Type == auth.TokenAccess` and reject refresh tokens outright.
    If a future change ever needs a third token flavor (e.g. a one-time
    WS "ticket" instead of putting the access token in the URL), extend
    the same `type` claim rather than adding a parallel token system.
13. **Mutating handlers that change state someone would want an audit
    trail for call `activity.Record` inline, at the same call site, not as
    a follow-up pass.** It was added retroactively once already (see
    the "Activity page" note below on why it was deferred the first
    time — don't repeat that mistake for the next mutating endpoint).
    `activity.Record` never returns an error to the caller; a logging
    failure must never fail the actual request.
14. **A DB write that depends on an external call succeeding (the daemon,
    in `ServerHandler.Create`) goes inside one `pgx.Tx`, and the external
    call happens *before* `tx.Commit()`, not after.** If the daemon call
    fails, `defer tx.Rollback(ctx)` undoes the server row and any claimed
    allocation automatically. Don't insert-then-call-then-cleanup-on-error
    manually — the deferred rollback pattern is shorter and can't leak a
    half-created row if a future code path adds an early return.
15. **Any path that comes from a client and touches the filesystem goes
    through `files.SafeJoin` first, no exceptions.** It normalizes the
    requested path as if rooted at `/` (so `Clean` collapses `../` before
    it ever gets joined to the real base directory), joins it under the
    server's data dir, then re-verifies with `filepath.Rel` that nothing
    escaped. Verified against `../../../etc/passwd`-style inputs by hand
    before wiring it into any handler — do the same sanity check again if
    this function is ever touched, since it's the entire security boundary
    between "browser typed a path" and "daemon process root-owns the
    host's real filesystem."
16. **Every `.sfield` in a form must be `input`, `textarea`, or `select` —
    `panel.css`'s `.sfield input, .sfield textarea` rule silently didn't
    cover `select`, so every dropdown (Create Server's Node/Egg/Allocation,
    Schedules' Action, Nodes' allocation-node picker) rendered as an
    unstyled native browser control instead of the dark theme. Fixed by
    adding `select` to the same rule plus a custom SVG-arrow via
    `appearance: none`. If a future form control type shows up in an
    `.sfield` (e.g. a native `<input type="date">` or a checkbox), check
    it against this same rule rather than assuming `.sfield` styling is
    automatic — it only covers what's explicitly listed.
17. **A handler-local `log.Printf` right before an `http.Error` 5xx/502
    response is what makes a live-production failure diagnosable later** —
    `ServerHandler.Create`'s two 502 branches (node client unavailable,
    daemon rejected/failed the create call) previously discarded the real
    Go error entirely, so the frontend only ever showed "POST /servers
    failed: 502" with no way to tell "wrong daemon token" from "wingsd
    unreachable" from "node has no stored token" apart. Added
    `log.Printf` at both sites (see `internal/scheduler`/`internal/ws` for
    the established `"<component>: <what> failed: %v"` format this
    follows). **Watch out**: when logging an error alongside a value that
    came from the same failed call, guard against a nil pointer — e.g.
    `daemonclient.CreateServer` returns `(nil, err)` on failure, so logging
    `daemonResp.Success` unconditionally would nil-deref exactly when
    `err != nil`, i.e. exactly the case the log line exists to describe.
    Branch on `err != nil` first and only touch the response value in the
    other branch.
18. **Every server-scoped handler must check admin-or-owner-or-subuser-
    with-permission — never just "is this request authenticated at all."**
    `auth.SubuserChecker.CanAccessServer`/`CanViewServer` are the two
    entry points (write-scoped action vs. read-only view); every handler
    that takes a server UUID from the URL should call one of them before
    doing anything, the same way `files.SafeJoin` is the one mandatory
    gate for filesystem paths (convention 15). This was written down
    *because* two handlers (`ServerHandler.Get`/`Power`) and both WS
    routes shipped for a long time with no server-level check at all —
    only proving "this JWT is valid," never "for *this* server." When
    adding a new `/servers/{uuid}/...` route, grep this file's RBAC
    section for the permission code that matches the action, and make
    sure the handler actually calls `CanAccessServer`/`CanViewServer`
    with it — don't assume "it's behind `auth.Middleware`" is enough,
    that only proves who's asking, never what they're allowed to touch.
19. **`claims.IsAdmin`-style bypasses inside a permission-check function
    must never be the only gate when a second, independent restriction
    (like API-key scoping) also applies.** `SubuserChecker.CanAccessServer`
    correctly short-circuits true for admins — that's right for "does
    this *user* have access to this server." But `Claims.HasKeyPermission`
    answers a different question ("is this *specific credential* allowed
    to do this"), and it has to be checked as its own separate condition
    at every call site, not merged into `CanAccessServer` itself. If it
    were merged and short-circuited the same way, a deliberately scoped-
    down admin API key would silently regain full access the moment the
    underlying user happened to be an admin — exactly the scenario
    scoping exists to prevent. Two independent questions need two
    independent checks, even when both eventually get `&&`-ed together
    at the call site.
20. **A JSONB permissions/scope column's "empty array" state should mean
    whatever preserves existing rows' behavior, not whatever seems most
    "secure by default."** `api_keys.permissions` already defaulted to
    `'[]'` for every key created before scoping existed; if empty-array
    had been chosen to mean "no permissions" (the more obviously "secure"
    reading), every previously-issued API key would have silently broken
    the moment this feature shipped, with no migration path since there's
    no way to distinguish "created before scoping" from "deliberately
    scoped to nothing" after the fact. Chose empty-array = unrestricted
    instead, specifically because it's what every existing row already
    means. When adding scoping to an existing nullable/default-empty
    column, work out what the *current* rows' behavior is first, then
    pick the encoding that doesn't silently change it.
21. **Releases are versioned now — every commit is not a "release."**
    Before this, `VersionHandler.CheckUpdate` compared raw commit SHAs
    against GitHub's `main`, so "update available" was true after
    *literally any* commit — there was no such thing as a deliberate
    release boundary, just a constant stream of builds. There's now a
    `VERSION` file at repo root (plain semver, e.g. `0.1.0`), embedded
    into the `panel` binary via the same `-ldflags -X` mechanism that
    already handled `commit`/`buildDate` (`scripts/panel.sh`'s
    `build_panel_binaries`), and `CheckUpdate` now fetches
    `raw.githubusercontent.com/.../main/VERSION` and compares *that*,
    not commit hashes. **The process this enables**: keep committing and
    pushing after each feature/fix like always — that's day-to-day work,
    not a release — but only bump `VERSION` and `git tag vX.Y.Z` at an
    actual milestone (a batch of related work that's been built, verified,
    and is worth calling a version), then push the tag
    (`git push origin vX.Y.Z`). Users only see "update available" when a
    tagged version bump lands on `main`, not on every commit in between.
    Patch (`0.1.x`) for bug/security fixes, minor (`0.x.0`) for new
    features, following ordinary semver; stay under `1.0.0` until the
    product is actually stable enough to promise compatibility.

## Roadmap — rough priority order

### Near-term (Server Detail's remaining tabs — only Databases is left)
- **Files tab follow-ups** — rename/upload/download/binary-detection are
  done (see above). What's still missing: `ReadFile`/`WriteFile` load the
  whole file into memory both in the daemon and the panel — fine for
  config files, would need streaming for anything large (logs, world
  saves, or an uploaded file bigger than available RAM). No drag-and-drop,
  just a plain file-picker button. No progress indicator on upload/download
  either — fine at config-file sizes, would matter once streaming is added.
- **Databases tab is real now** — no longer the last placeholder. Went
  with the same architecture Pterodactyl itself uses: an admin registers
  one or more **database hosts** (a reachable MySQL/MariaDB server the
  panel can connect to as an admin user — new `database_hosts` table,
  migration `0006`, admin credentials AES-GCM encrypted like daemon
  tokens), then any user with `databases.write` on a server can
  provision a database on one of those hosts from the Databases tab.
  New `internal/mysqlhost` package wraps `database/sql` +
  `go-sql-driver/mysql`: `Provision` runs `CREATE DATABASE` / `CREATE
  USER` / `GRANT ALL` / `FLUSH PRIVILEGES`, `Deprovision` runs the
  `DROP` equivalents, `Ping` just opens+closes a connection (used to
  validate a new host's credentials at registration time, so a typo'd
  password is caught immediately instead of surfacing later as a
  mysterious create-database failure).
  - **Identifier safety**: MySQL doesn't support parameterized
    identifiers (`?` placeholders only work for values, never for
    database/user/table names), so database and user names are
    generated server-side from the server's numeric ID plus a
    strictly-sanitized suffix (`[^a-zA-Z0-9_]` stripped, truncated to
    fit MySQL's 32-char username limit), and `mysqlhost.ValidIdentifier`
    re-validates against `^[a-zA-Z0-9_]+$` immediately before any
    identifier is concatenated into a DDL string — never trust that the
    generation step alone was enough, check again right at the point of
    use.
  - **Password safety**: the per-database password is always generated
    by the panel itself (same `generateToken`/hex-encoded-random-bytes
    helper `NodeHandler.Create` already used for daemon tokens), never
    user input, so it can only ever contain `[0-9a-f]` — safe to embed
    directly in the `CREATE USER ... IDENTIFIED BY '...'` string
    alongside the validated identifiers, since it can never contain a
    quote or backslash. This is *why* it's safe to build these
    statements with string concatenation instead of parameter binding,
    even though string-built SQL is normally the thing to avoid — the
    safety comes from fully controlling both value spaces (identifiers
    validated against a strict pattern, password from a strict
    character set), not from the concatenation itself being fine.
  - Passwords are stored encrypted (same AES-GCM/`internal/crypto`
    pattern as everywhere else) but — unlike an API key — **decrypted
    and returned on every `List` call**, not shown once. A database
    password isn't a bearer credential for *this panel*, it's a
    credential the owner needs indefinitely to configure their actual
    application (bot config, `.env` file, etc.), so "shown once" would
    make the feature useless. Frontend hides it behind a "Show"/"Copy"
    toggle by default so it's not just sitting in plain text on screen.
  - Admin-only: registering/deleting database hosts. Owner-or-subuser-
    with-`databases.read`/`write`: everything scoped to one server.
    `DatabaseHostHandler.Delete` refuses if any `server_databases` row
    still references that host (same "still has children" guard as
    node deletion).
- **Schedules follow-ups** — `backup` task actions are still schema/UI-shaped
  but no-op in `scheduler.execute()` (needs backup infrastructure that
  doesn't exist yet). The one-task-per-schedule assumption in the UI
  (`ScheduleManager.tsx` only ever creates a single task) is a frontend
  simplification, not a backend limit — `schedule_tasks` already supports
  an ordered sequence with per-task offsets; a "multi-step schedule" UI
  is additive whenever it's worth building.
- **Allocations got port-range bulk-add and delete** — `AllocationHandler.Create`
  now accepts an optional `port_end`; when set, it loops `port..port_end`
  inside one transaction, `INSERT ... ON CONFLICT (node_id, ip, port) DO
  NOTHING` per port (so re-adding an overlapping range is a no-op instead
  of a hard failure), and reports back how many were actually created vs.
  requested — capped at 1000 ports/request as a sanity limit, not a real
  scaling concern. Added `DELETE /allocations/{id}`, which never existed
  before (a mistaken entry was permanent); it only deletes when
  `server_id IS NULL`, so an allocation currently backing a running
  server can't be yanked out from under it. Still no validation that the
  IP actually belongs to the node (nothing stops an operator from typing
  a wrong IP) and no reservation system for "hold this range for later" —
  low priority since it's an admin-only, low-frequency operation.

### Mid-term (real functionality gaps, not just missing UI)
- gRPC migration for the daemon protocol (proto file is complete,
  `daemonclient`/`daemon/internal/api` are still the HTTP/WS stand-in) —
  low urgency, only matters once file-manager/backup streaming RPCs need
  the bidirectional-stream ergonomics gRPC gives you for free.

### Later / polish
- Frontend i18n (the installer got RU/EN; the SPA itself is still
  English-only — if the Russian-speaking installer experience matters,
  the dashboard probably should too, eventually).
- SFTP server on wingsd (mentioned in the original spec, not started).
- `npm audit` flags `esbuild`/`vite` (moderate/high, dev-server-only —
  lets any website the developer visits send requests to the local Vite
  dev server and read the response; doesn't affect the production
  build/nginx-served output at all). Fix requires a major Vite version
  bump (`npm audit fix --force` → `vite@8`); deliberately not done blind
  given the breaking-change risk to the whole build pipeline — worth
  doing as its own dedicated upgrade-and-test pass, not bundled in.

## Things I keep having to re-explain to myself — write them down once

- `scripts/database.sh`'s `wait_for_postgres` exists because of a real
  incident: VPS images built from container templates ship
  `/usr/sbin/policy-rc.d` that silently blocks `postgresql-16`'s postinst
  from creating a cluster. `neutralize_policy_rc_d` (in `lib.sh`) fixes the
  root cause; `wait_for_postgres`'s cluster-creation fallback is defense in
  depth for hosts that were already broken before that fix existed. Don't
  "simplify" either of these away — they're both load-bearing.
- The frontend never had a login screen until a user hit a live 401 in
  production and asked "why doesn't anything work" — the lesson: when
  scaffolding a new page that hits an authenticated endpoint, check there's
  actually a way to get a token into `localStorage` before calling it done.
- `install.sh`'s self-clone-and-exec only fires when `scripts/lib.sh` isn't
  next to it (i.e. running via `bash <(curl ...)` with nothing checked out
  locally) — don't add prompts before that check runs, they'd never be
  reached in the one-liner case since the script re-execs itself immediately.
- Both WS endpoints were completely unauthenticated until this pass — a
  leftover from the original scaffold, which even said so in a comment
  that got stripped during the no-comments cleanup (the gap itself
  outlived the comment describing it, which is exactly the risk of
  removing "TODO"-shaped comments without also checking whether the TODO
  ever got done). Found it while wiring Console, since sending arbitrary
  console commands with zero auth is a much sharper problem than
  read-only stats were. Worth periodically grepping for other
  `r.Get`/`r.Post` calls registered directly on the base router `r`
  instead of inside the authenticated `r.Group` — that's the tell.
- `docker.Manager.CreateContainer` used to always set `Cmd` to
  `/bin/sh -c "<startup_command>"`, even when `StartupCommand` was `""` —
  which produces `/bin/sh -c ""`, a container that starts and immediately
  exits. This silently broke any egg meant to run via the image's own
  `ENTRYPOINT` (e.g. `itzg/minecraft-server`, which needs `Cmd` left nil
  entirely). Found it while seeding the Minecraft egg for the create-server
  flow — the fix is a three-line `if spec.StartupCommand != ""` guard.
  Worth remembering: "override unless empty" is the wrong default when
  wrapping someone else's Docker image; the image's own defaults are
  usually the point of using it.
- **A four-hex-digit unicode escape typed into a `Write`/`Edit` tool call's
  content is not guaranteed to survive as literal source text** — it can
  get decoded into the actual raw codepoint/byte before the file hits
  disk. Tried to write a `looksBinary()` helper this way, targeting the
  NUL codepoint, and it corrupted the file with a real raw NUL byte
  instead of six characters of TypeScript source (confirmed via `grep`
  reporting "binary file matches" on what should have been a `.tsx`
  file — and this exact bullet point corrupted this very file, `add.md`,
  the same way on the first attempt to write it down). Fix: build the
  character at runtime instead — `String.fromCharCode(0)` — any time a
  control character or non-printable codepoint needs to appear literally
  in generated source, rather than typing the escape sequence into a
  tool-call parameter.
- Any "if the env/config file already exists, skip everything" guard
  (`write_panel_env`'s original shape) is a trap for anything that needs to
  run on *every* install, not just the first one — found this the hard way
  with migrations: they were silently skipped on every update because they
  were bolted onto the same early-return as secret generation. When adding
  a new "only do this once" step, ask separately "does this specific piece
  need to happen once, or every time" — don't assume the whole function is
  one atomic once-only unit.
- **`r.Use(someMiddleware)` on chi's base router applies to every route
  ever registered on it, including ones added later in the same function**
  — same root cause as the WS-auth gap above, hit again while adding
  `wingsd`'s `/healthz`: `RequireDaemonToken` was a top-level `r.Use`, so
  a health-check endpoint meant to work *without* a token would have
  required one anyway just by being registered on the same router. Fixed
  by moving the token check into an `r.Group(...)` that only wraps the
  routes that actually need auth, with `/healthz` registered outside it.
  When adding any endpoint that's deliberately public (health checks,
  webhooks with their own signature scheme), register it before/outside
  the auth group rather than assuming a later route can opt out of an
  earlier blanket `r.Use`.
- **Found and fixed the real cause of a live `failed to read servers (500)`
  report**: `models.Server.ContainerID` was declared as a non-pointer
  `string`, but `servers.container_id` in the schema is nullable
  (`TEXT`, no `NOT NULL`/`DEFAULT`) and — confirmed via a repo-wide
  grep — is **never written by any `INSERT`/`UPDATE` anywhere in the
  codebase**. Every server row has had `container_id = NULL` since the
  table was created. pgx's `Scan()` fails at runtime the moment it tries
  to scan a SQL `NULL` into a non-pointer Go field, which is exactly
  the "failed to read servers" branch in `ServerHandler.List`/`Get`
  (as opposed to "failed to list servers", the query-failure branch —
  worth keeping those two error strings distinct for exactly this kind
  of diagnosis). This bug has presumably existed since those handlers
  were first written, but stayed dormant all session because the
  `servers` table was empty while the daemon 401/502 issues were being
  debugged — it only surfaced once the node/token fixes let the user
  actually create a server for the first time. Fixed by changing the
  field to `*string`, matching the schema's real nullability; updated
  the matching frontend type (`container_id?: string | null`) for
  consistency, though nothing renders it. Checked `Description` for the
  same risk (also nullable, also non-pointer) but confirmed it's not
  currently exposed: `CreateServerForm.tsx` has no description input, so
  Go's JSON decode always defaults that field to `""`, never `NULL`, in
  current practice — left as-is rather than defensively changing a field
  that isn't actually broken yet. General lesson: a nullable DB column
  scanned into a non-pointer Go field is a time bomb that only detonates
  once a real NULL row exists to scan, which can be long after the code
  was written and reviewed.
- **`could not connect to that MySQL/MariaDB host with the given
  credentials` was reported for a plain `connection refused`** — a pure
  TCP-level dial failure (nothing listening on that host/port at all,
  before any credentials are even sent) was being worded identically to
  an actual auth rejection, which misleads the user into suspecting
  their username/password when the real problem is the remote MySQL/
  MariaDB isn't reachable (down, bound to `127.0.0.1` only, wrong port,
  firewalled). Not a panel bug — the dial genuinely was refused, that
  part is infra on the DB host's side — but the wording was. Added
  `mysqlhost.DescribeConnectError`, which uses `errors.As` to tell a
  `*net.OpError` (never reached the host, credentials irrelevant) apart
  from a MySQL `Error 1045` (reached the host, credentials specifically
  rejected) apart from anything else, and phrases each case accordingly.
- **Live server stats/online-offline status could permanently desync from
  reality after any WebSocket hiccup** — two compounding bugs, both hit by
  the same underlying complaint ("после F5 стопится", stats/online-offline
  freeze and don't recover):
  1. Frontend: `connectServerSocket` was opened exactly once per server
     with zero reconnect logic anywhere (`ServerList.tsx`, `ServerView.tsx`).
     Any drop — daemon restart, laptop sleep, flaky wifi, a proxy's WS idle
     timeout, or even just a briefly-stale access token at the exact moment
     of connecting — left the socket dead forever with no retry, freezing
     `live` stats and the online/offline badge at their last known value
     until a full manual page reload. Fixed by adding
     `connectServerSocketWithRetry` (`client.ts`) — capped exponential
     backoff, and a `tryRefresh()` call before every reconnect attempt
     (not just the first) so a stale token can't wedge the retry loop
     permanently either.
  2. Daemon (the actual root cause of state going *wrong*, not just
     stale): `Handlers.Stats` (`daemon/internal/api/handlers.go`) called
     `Docker.Stats()` — live container stats — *before* `InspectState()`.
     The moment a container isn't running (stopped, restarting, removed),
     the stats call fails and the handler 502'd immediately, never
     reaching the `InspectState` call that would've reported "offline".
     `Hub.pollStats` on the panel side just `continue`s past any
     `FetchStats` error, so the panel never learned the server had
     actually gone offline — it kept broadcasting nothing, and the UI sat
     on the last "running" snapshot indefinitely. Fixed by checking state
     first and always reporting it; stats are only fetched (and only
     required to succeed) when the container is confirmed running.
- **Console tab: two real bugs plus one root-cause omission, all under
  the umbrella of "console просто не работает, no idea why"**:
  1. Daemon (`console.Hub.Serve`) and panel (`Hub.ServeConsoleSocket`)
     both failed completely silently on the client side whenever
     `Docker.Attach`/`LogsFollow` errored (most commonly: the server
     just isn't running — stopped, still installing, or its container
     was removed) or the panel couldn't dial the node daemon at all.
     The browser's console WS would connect fine (the panel→browser
     upgrade always succeeds) and then just sit there forever showing
     nothing, with the actual failure reason only ever reaching a
     server-side `log.Printf` the user never sees. Fixed by writing an
     explicit `[console] ...` text message over the WS before closing
     in every one of those failure paths, so the reason is now visible
     directly in the console output pane instead of vanishing into a
     log file on a machine the user isn't looking at.
  2. Frontend: `connectConsoleSocket` (a bare `WebSocket`) was stored in
     a ref and `sendCommand` called `.send()` on it directly. A
     `WebSocket.send()` while `readyState` is still `CONNECTING` throws
     synchronously — and since the panel-side WS upgrade completes
     locally before the panel has even started dialing the node daemon,
     there's a real window where the browser socket reports "open" via
     browser semantics before it can usefully carry a command. Typing
     and hitting enter during that window silently ate the keystroke
     (exception thrown before `setCommand('')`, so the input just sat
     there looking unresponsive) with zero indication anything went
     wrong. Fixed by wrapping the console socket the same way as the
     stats socket: `connectConsoleSocketWithRetry` returns a small
     `{ send, close }` handle that only actually calls `.send()` when
     `readyState === OPEN`, exposes connection status via a callback,
     and reconnects with capped backoff (+ a token refresh per attempt)
     on any drop, matching the resilience already added for the stats
     socket. The UI now shows "Connecting…" and disables the input
     until the daemon side is actually live, instead of pretending to
     be ready the instant the tab opens.
- **v0.1.1**: patch release on top of v0.1.0, bug fixes only, no new
  features. Everything that shipped between the two tags: the
  `ContainerID` nullable-pointer crash fixed ("failed to read servers
  (500)"), clearer database-host connect errors (refused-connection
  vs. bad credentials), the daemon `Stats` handler ordering fix so
  online/offline no longer freezes on the last known state once a
  server actually stops, WebSocket reconnect-with-backoff for both the
  live-stats and console sockets, and the console tab no longer failing
  completely silently (attach/log-follow/dial errors now surface as a
  visible `[console] ...` line instead of vanishing into a server log).
- Added `website/` — a standalone marketing/landing site for Roost, built to
  be dogfooded on the panel itself: `app.py` is a zero-dependency stdlib
  `http.server` static file server (reads `PORT` from the environment), so it
  runs as-is on the existing "Python: Website" egg with an empty
  `requirements.txt`. `public/` holds the actual page — same dark palette and
  `--pink`/blob-gradient language as `panel.css`, plus scroll-reveal,
  animated stat counters, a typing-effect terminal block, an infinite
  marquee, and a feature-comparison table, all gated behind
  `prefers-reduced-motion` like the panel's own animation work. Verified
  with a real headless-browser run (Playwright, desktop + mobile viewports,
  zero console errors) rather than just reading the code.
- Rewrote the root `README.md` top to bottom: it had drifted badly out of
  date (still describing files/console/schedules/databases/subusers as
  "not implemented yet" when all of them ship), so this is a straight
  rewrite reflecting current reality plus badges, a feature table, and the
  same competitive-comparison framing as the new landing page, instead of
  a rebrand copy grafted onto stale content.
- **Full uninstall (`uninstall_full`) left PostgreSQL in a state that broke
  every subsequent reinstall** — confirmed live on a real box, not
  theoretical. `apt-get purge` named only the `postgresql`/`postgresql-contrib`
  metapackages; the actual versioned engine (`postgresql-16` on Ubuntu
  24.04, plus `postgresql-client-16`/`postgresql-common`) is a separate
  package that `apt-get autoremove` didn't reliably catch either — so it
  stayed fully installed, with its cluster config still sitting in
  `/etc/postgresql/16/main`, right next to the unconditional
  `rm -rf /var/lib/postgresql` two lines down that deletes the actual data
  directory. Net result: a cluster whose config exists (so `pg_lsclusters`
  reports it and the installer's recovery path tries to `service
  postgresql restart` it) but whose data directory is gone, which can
  never come back up. On the next `install.sh` run, `postgresql`/
  `postgresql-contrib` were already "installed" as far as apt was
  concerned, so nothing re-triggered `pg_createcluster`, and
  `wait_for_postgres` failed outright. Fixed by discovering every
  installed `postgresql*` package with `dpkg-query` and purging all of
  them by name instead of guessing two, plus explicitly removing
  `/etc/postgresql`, `/etc/postgresql-common` and `/var/log/postgresql`
  in the cleanup `rm -rf` rather than trusting purge to have caught
  everything.
- Replaced every remaining `superbodik/sbPanel` reference with
  `superbodik/Roost` (backend's default `PANEL_UPDATE_REPO`, the
  frontend's node-install-command URL, `install.sh`'s self-clone URL,
  README badges/links, and the marketing site) to match the GitHub repo
  rename — the version-check and node-install-command features would
  otherwise have kept pointing at the old, redirected URL indefinitely.
- **Fresh installs broke at "Failed to create admin user" with `PANEL_JWT_SECRET
  is not set to a real secret`** — confirmed live. `create_admin_interactive`
  invoked `panel-admin` with only `PANEL_DATABASE_URL` set in its environment
  (extracted from `panel.env` with a one-off `grep`), but `panel-admin` calls
  the exact same `config.Load()` as the main `panel` binary, which refuses to
  start unless `PANEL_JWT_SECRET` is also a real, non-default secret — and
  that variable was simply never passed through. `panel.service` gets the
  full file via `EnvironmentFile=`; the interactive admin-creation step
  didn't have an equivalent and only cherry-picked one variable. Fixed by
  running `panel-admin` inside a `( set -a; source "$PANEL_ENV_FILE"; ... )`
  subshell instead, so it gets every variable panel.env actually defines,
  matching what the systemd unit already does. Verified the exact shell
  logic in isolation with a stand-in panel-admin script before shipping.
- Added custom-domain support for servers ("Domains" tab in server
  settings): `server_domains` table (0007 migration), `ServerDomainHandler`
  (list/create/delete, gated by new `domains.read`/`domains.write`
  permissions, same owner-or-subuser pattern as every other per-server
  resource), and a `DomainManager.tsx` component modeled directly on
  `DatabaseManager.tsx`. The actual proxying happens on the node: a new
  `daemon/internal/proxy` package writes an nginx vhost
  (`/etc/nginx/sites-available/roost-<domain>.conf`, symlinked into
  `sites-enabled`) that reverse-proxies the domain to the server's primary
  allocation port on `127.0.0.1`, reloads nginx (`nginx -t` first, so a bad
  config can't take the node's nginx down), and then runs
  `certbot --nginx -d <domain> --non-interactive --redirect` — the same
  certbot invocation pattern `scripts/domain.sh` already uses for the
  panel's own domain, just triggered at runtime instead of install time.
  If certbot fails (DNS not pointed there yet, rate-limited, whatever) the
  domain is recorded as `tls_status: http_only` rather than failing the
  whole request — plain HTTP still works, and the user can just delete and
  re-add once DNS is fixed. Domain names are validated with the same strict
  hostname regex on both the panel and the daemon (defense in depth, since
  the string ends up in a shell arg to certbot and a filename on the node).
  `scripts/daemon.sh` now installs nginx + certbot on node install, since
  neither was there before — the panel's installer already had both, but
  only for the panel's own reverse proxy, not per-node. Verified the new
  tab end-to-end in a real browser (Playwright driving the actual SPA
  through Manage -> Domains, with the backend mocked) rather than trusting
  the build alone; both listed domains rendered with correct TLS status
  text and the add-domain form worked as expected.
- **v0.2.0**: minor release on top of v0.1.1 — one new feature (custom
  domains) plus the fixes that landed alongside it. Everything since
  v0.1.1: the sbPanel -> Roost rename across every remaining reference
  (following the actual GitHub repo rename), the full-uninstall
  PostgreSQL-purge fix, the admin-creation JWT-secret fix that was
  outright blocking fresh installs, the Roost marketing site + README
  rebrand, and per-server custom domains with nginx+certbot reverse
  proxying on the node.
- **Adding a custom domain died with `Client.Timeout exceeded while
  awaiting headers` (502)** — confirmed live, right after shipping the
  domains feature. Three separate timeout ceilings were all shorter than
  certbot's ACME validation can legitimately take, and fixing only the
  most obvious one wasn't enough:
  1. `daemonclient.Client`'s shared `http.Client` had a hardcoded
     `Timeout: 15 * time.Second` used for every call type — including
     `AddDomain`, which shells out to certbot on the node and can easily
     run past 15s. `http.Client.Timeout` caps the whole round trip
     regardless of the context deadline the caller passes, so the
     handler's own `context.WithTimeout(r.Context(), 90*time.Second)`
     never got a chance to matter. Added a second `longHTTP` client
     (120s) used only by `AddDomain`/`RemoveDomain`.
  2. Both the panel's and the daemon's chi routers had
     `middleware.Timeout(...)` applied via `r.Use` on the *root* router,
     before any route groups existed — the exact same "blanket `r.Use`
     catches every route registered on this router, including ones added
     later" class of bug documented above for `RequireDaemonToken` and
     `/healthz`. Since a context's deadline can only ever shrink when a
     child derives from it (never extend past the parent's), the
     handler's own longer timeout was silently capped back down to
     whatever the root middleware had already set (30s on the panel, 60s
     on the daemon) — my fix to daemonclient's timeout in isolation
     wouldn't have been enough on its own. Fixed by moving
     `middleware.Timeout` off the root router on both sides and applying
     it per-group instead: the existing routes keep their original
     30s/60s ceiling in one group, and the domain create/delete routes
     get their own 150s-timeout group sitting alongside it. Verified both
     routers still construct without a chi route-registration panic via a
     throwaway `NewRouter` smoke test on each side before shipping.
- **"+ Folder" in the Files tab did nothing — no error, no dialog, no new
  folder"**. Traced the entire path (button → API → daemon → `os.MkdirAll`)
  and even wrote a throwaway test hitting `files.Mkdir` directly against a
  real filesystem — the backend logic was completely correct. The actual
  bug was `window.prompt()`/`window.confirm()` themselves: several mobile
  browsers and in-app webviews (Telegram's in-app browser, some Android
  WebViews, PWA contexts) don't support native synchronous dialogs at all
  and silently return `null`/`false` instead of showing anything — which
  `handleNewFolder`'s `if (!name) return;` interprets as "user cancelled,"
  producing exactly "nothing happens" with zero error surfaced. Replaced
  both the new-folder prompt and the rename prompt (identical pattern,
  identical risk) with inline forms in the Files tab itself instead of
  native dialogs — verified end-to-end in a real browser (Playwright,
  mocked API) that submitting the form actually calls
  `POST .../files/directory?path=...` with the right path. Left the
  delete confirmation as a native `window.confirm` for now since it wasn't
  the reported bug and touching every dialog in the app is a separate,
  larger cleanup.
- Implemented real SFTP access — `ssh_keys` and port 2022 had been reserved
  in the schema/firewall since the start but never actually built. Design:
  - **Account page**: users register their own SSH public keys
    (`SSHKeyHandler` — list/create/delete, fingerprint computed server-side
    via `ssh.FingerprintSHA256` at upload time so it's never trusted from
    the client).
  - **wingsd** embeds a real SSH/SFTP server (`daemon/internal/sftpd`,
    `golang.org/x/crypto/ssh` + `github.com/pkg/sftp`'s request-server API)
    on port 2022. `Filecmd`/`Filewrite`/`Filelist` all route every path
    through the *existing* `files.SafeJoin` used by the REST file manager,
    so both surfaces share the exact same path-traversal protection rather
    than duplicating it.
  - **Auth is a live callback, not a pushed access list**: on every SSH
    connection, wingsd POSTs `{username, fingerprint}` to a new panel
    endpoint, `POST /api/v1/internal/sftp/authenticate`, presenting its own
    daemon token as `Authorization: Bearer`. The username is
    `<panel-username>.<server-uuid_short>` (Pterodactyl's convention) —
    the daemon itself doesn't know the full server UUID or which servers
    exist on the panel's side, only the short ID a human typed, so the
    panel resolves `uuid_short -> server`, decrypts *that specific node's*
    stored `daemon_token_encrypted` and constant-time-compares it against
    the presented token (proving "this really is the node that owns this
    server" without needing a separate node-identity field), checks the
    ssh_key fingerprint against that user's registered keys, and runs the
    exact same `SubuserChecker.CanAccessServer` gate every other per-server
    resource uses. The response carries the full server UUID back down so
    wingsd can resolve `ServerVolumePath` — the daemon holds zero
    credentials or access lists locally, permission changes take effect on
    the very next login with no push/sync step anywhere.
  - Verified with a real integration test (`daemon/internal/sftpd/server_test.go`):
    spins up the actual SSH server against a stub panel, connects a real
    SSH/SFTP client with a generated keypair, and exercises ReadDir/Mkdir/
    Create+Write/Remove against the real filesystem plus a path-traversal
    rejection check — not just "it compiles."
  - Also fixed a real, pre-existing installer gap while wiring this up: the
    firewall rule for port 2022 was unconditional (opened even on
    panel-only installs where nothing ever listens on it) — moved it into
    the same daemon-only conditional as port 8443. `scripts/daemon.sh` now
    installs asks for/accepts `WINGSD_PANEL_URL` (needed for the callback)
    the same way it already does for the daemon token, and the frontend's
    node-install one-liner now includes `WINGSD_PANEL_URL=<window.location.origin>`
    automatically.
  - Sanity-checked an assumption about `daemon/go.mod` along the way:
    it already said `go 1.25.0` before this session touched it (confirmed
    via `git show HEAD:daemon/go.mod`), so the earlier "always re-pin to
    1.22" note in this file was based on an incomplete picture — Go's
    default `GOTOOLCHAIN=auto` (nothing in `scripts/toolchain.sh` restricts
    it) transparently downloads whatever toolchain a go.mod actually
    requires, so a provisioned 1.22.5 install is not actually stuck or
    broken by a higher `go` directive. Left it at whatever `go mod tidy`
    naturally resolved to rather than fighting it.
- Implemented real backups — `server_backups` had a fully-designed schema
  (`ignored_files`, `bytes`, `checksum`, `is_successful`, `completed_at`)
  since the very first migration, referenced in `docs/DATABASE.md`'s own
  entity notes, but had zero handler, zero daemon logic, and zero UI
  anywhere — the exact same "reserved but never built" shape as SFTP.
  - **Daemon** (`daemon/internal/backup`): `Create` walks the server's
    volume directory and streams it straight through `tar` → `gzip` →
    the destination file *and* a SHA-256 hasher simultaneously via
    `io.MultiWriter` (no double-buffering an entire server's files in
    memory), matching `ignored_files` glob patterns against both the
    full relative path and the basename so a pattern like `cache` (a
    directory) or `*.log` (an extension) both work as expected. `Restore`
    extracts back into the server directory using the exact same
    "prepend a leading slash, then `filepath.Clean`, then verify the
    result via `filepath.Rel`" containment trick `files.SafeJoin` already
    uses elsewhere in this codebase — a hostile or corrupted archive
    can't write outside the server's own directory (zip-slip class of
    bug). Verified both properties with real tests, not just reasoning
    about the code: `TestCreateAndRestore` round-trips actual files
    through create→restore and checks the SHA-256 the code reports
    against an independently-computed one; `TestRestoreRejectsPathTraversal`
    builds a hand-crafted malicious tar entry (`../../../../etc/passwd`)
    and confirms it lands safely inside the restore directory instead of
    escaping to the real filesystem root — this test caught a wrong
    assumption on the first pass (I originally expected `Restore` to
    return an error for such an entry; the actual, correct, already-
    proven-safe behavior is to silently neutralize and contain it, same
    as `SafeJoin` does — the test forced the assertion to match reality
    instead of my initial guess).
  - **Panel**: `ServerBackupHandler` (list/create/restore/delete/download),
    gated by new `backups.read`/`backups.write` permissions and the
    existing `backup_limit` column (quota was already on `servers`, also
    unused until now). Create/restore/download sit in the same 150-second
    timeout group already carved out for the domains feature's certbot
    calls, since archiving a large server is exactly the same "legitimately
    slow node-side operation" shape — reused the pattern instead of
    re-deriving it. Extracted the daemon-token-verification logic
    (decrypt this specific node's stored token, constant-time-compare
    against what was presented) out of the SFTP auth handler into a
    small shared `internal/daemonauth` package, since this handler needed
    the exact same check a second time — a genuine reuse opportunity, not
    a hypothetical one.
  - **Scheduler**: added a `"backup"` action alongside the existing
    `"power"`/`"command"` ones, so "nightly backup" is a real schedulable
    task now instead of just power actions and console commands.
  - **Frontend**: new "Backups" tab (`BackupManager.tsx`, modeled on
    `DomainManager.tsx`), and a third schedulable task type in
    `ScheduleManager.tsx`. Failed backups show as "Failed" with
    Download/Restore disabled rather than silently pretending to be
    downloadable — `is_successful` was always meant to record failures
    for visibility, not just gate a success flag nobody reads.
  - Verified the whole chain end-to-end with a real browser (Playwright,
    mocked API): created a backup through the actual form, confirmed the
    right payload reached the API, and confirmed a failed backup renders
    with disabled actions instead of a broken-looking enabled button.
- Implemented real server suspension — `servers.is_suspended` and the
  `'suspended'` status enum value existed since the first migration
  (`StatusSuspended` in the Go model, `'Suspended'` label already wired up
  in the frontend's `StatusBadge`), but nothing anywhere ever set
  `is_suspended` to true, and `Power` never checked it — a server that was
  somehow marked suspended could still be started normally. Added
  admin-only `POST /servers/{uuid}/suspend` / `.../unsuspend`: suspending
  sets `is_suspended = true` and `status = 'suspended'`, and best-effort
  stops the container immediately via the daemon (logged, not fatal, if
  the node is unreachable — the DB flag is the actual source of truth for
  blocking future starts, not whether the stop call happened to land).
  `Power` now rejects `start`/`restart` with 403 while suspended; `stop`/
  `kill` stay allowed. Also discovered along the way that `servers.status`
  in the DB is otherwise *never* updated after creation — the "running"/
  "stopping" the UI shows day-to-day is a purely client-side illusion
  from the live-stats WebSocket that reverts to whatever's in the DB on
  reload. Didn't fix that separate, pre-existing gap here (syncing live
  daemon state back into the DB is its own project), just made sure my
  own suspend/unsuspend writes land in the one column that's supposed to
  reflect it. Frontend: a Suspend/Unsuspend row in the server's danger
  zone, Start/Restart disabled with an explanatory tooltip while
  suspended, matching the backend enforcement instead of just letting the
  click 403 with no context. Verified in a real browser: clicking Suspend
  actually disables Start/Restart and flips the button to "Unsuspend".
- **v0.3.0**: three real features that had been sitting in the schema as
  dead columns/tables since day one, all now actually built and shipped —
  SFTP (ssh_keys + port 2022), server backups (server_backups + backup_limit),
  and server suspension (is_suspended + the 'suspended' status). Plus two
  real bug fixes since v0.2.0: the domains-creation timeout chain, and
  "+ Folder" silently doing nothing on mobile/webview browsers.
- Implemented node capacity and visibility enforcement — `nodes.is_public`,
  `maintenance_mode`, `memory_overallocate`, and `disk_overallocate` were
  all real columns in the schema since the first migration (the last two
  explicitly documented in `docs/DATABASE.md` as "letting an admin
  deliberately sell more resources than physically exist"), but the panel
  never let anyone set `is_public`/`maintenance_mode` (no field in the
  update request at all), never filtered node visibility by it, and never
  once checked capacity against any of these numbers when placing a
  server — you could create as many servers as you wanted on a node
  requesting far more memory/disk than it had, with zero admission
  control. `models.Node` itself turned out to be entirely orphaned code
  (never instantiated anywhere — `node_handler.go` has always used its
  own private `nodeSummary` struct instead), which is presumably how this
  drifted so far without anyone noticing.
  - `NodeHandler.List` now filters to `is_public = true` for non-admins
    (the same endpoint doubles as the admin management view and the
    node-picker regular users see in the create-server form, so this one
    filter covers both call sites for free) and returns the overallocation/
    visibility fields; `Create`/`Update` now accept and persist all of
    them.
  - `ServerHandler.Create` now rejects placing a server on a node that's
    in maintenance mode, rejects a private node for non-admins, and
    checks actual memory/disk capacity — `used + requested <= total *
    (100 + overallocate%) / 100` — before ever calling the daemon. The
    capacity arithmetic is pulled out into a small `effectiveCapacity`
    helper with real table-driven tests (including the overallocation
    percentage actually changing the result, and a used+requested pair
    right on either side of the boundary) rather than trusting inline
    integer division in an HTTP handler untested.
  - Frontend: the Nodes page's edit panel gained overallocation inputs and
    Public/Maintenance toggles, and the node list shows "Private"/
    "Maintenance" badges inline so this state is visible without expanding
    every row. Verified in a real browser that toggling and saving sends
    the full updated payload to the API.
- **v0.3.1**: patch release — node capacity/visibility/maintenance-mode
  enforcement (is_public, maintenance_mode, memory_overallocate,
  disk_overallocate were all real schema columns doing nothing until now).
- Implemented egg-variable validation — `egg_variables.rules` (the
  Laravel-style DSL, e.g. `required|string|max:20`, `in:TRUE,FALSE` for
  the Minecraft EULA flag) was read from the DB and returned to the
  frontend since day one, but literally nothing ever validated a
  submitted value against it, client or server. A user (or a scoped API
  key calling the endpoint directly) could submit an empty value for a
  `required` field, garbage text for an `integer` field, or an arbitrary
  string where `in:TRUE,FALSE` demanded one of two exact values, and the
  panel would pass it straight through to the container as an environment
  variable with zero pushback — the egg's startup script would just have
  to cope. Separately, `is_editable` was only enforced by disabling the
  input in the React form, which is meaningless against a direct API call;
  nothing stopped overriding a non-editable variable server-side.
  - New `internal/eggvars` package: a small rule parser/validator
    (`required`, `string`, `integer`, `numeric`, `boolean`, `min:N`,
    `max:N`, `in:a,b,c`, with `nullable`/`sometimes` as accepted no-ops),
    with a real table-driven test for every rule type plus the
    empty-and-optional short-circuit case.
  - `ServerHandler.Create` now loads the egg's variables, forces
    non-editable ones back to their `default_value` regardless of what
    was submitted, and validates every editable one's value against its
    rules before ever dialing the daemon — a bad value now fails fast
    with a specific 400 instead of silently becoming a broken container.
  - Frontend: the create-server form now shows each variable's rule
    string as a hint and sets the native `required` attribute when the
    rules include it, so most mistakes get caught before the request
    even goes out, not just after.
- **Found and fixed two real TOCTOU races in code from this very session**
  (asked to sweep for bugs/holes before adding more features, so audited
  the recent additions specifically rather than assuming they were solid
  just because they'd shipped): both the node-capacity check in
  `ServerHandler.Create` and the `backup_limit` check in
  `ServerBackupHandler.Create` read a `count`/`SUM` via a plain
  `h.DB.QueryRow` *outside* any transaction, then inserted separately —
  two concurrent requests hitting the same node/server would both read
  the same pre-insert numbers, both pass the check, and both succeed,
  together exceeding the limit that was just added. Classic
  check-then-act race, and a more consequential one for node capacity
  specifically (double-booking a node's memory could genuinely
  oversubscribe it, not just a minor quota miscount). Fixed both by
  moving the check inside a transaction that locks the relevant row
  (`SELECT ... FOR UPDATE` on the node/server/user row) before reading
  the count and before inserting — under READ COMMITTED, a second
  concurrent transaction's `FOR UPDATE` blocks until the first commits,
  and its subsequent count read then correctly sees the first
  transaction's now-committed row. For servers, this meant moving the
  node-capacity, server-limit, and insert all into the same transaction
  that already spanned the daemon call (extending an existing pattern
  rather than introducing a new one). For backups, used a short-lived
  transaction that commits *before* the slow tar/gzip daemon call rather
  than holding the lock for the whole backup duration, since unlike
  server creation there was no existing precedent for holding the tx
  open across the daemon round-trip here.
