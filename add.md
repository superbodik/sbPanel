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
  usually newer than what's pinned for fresh installs.

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

## Roadmap — rough priority order

### Near-term (Server Detail's remaining tabs — only Databases is left)
- **Files tab follow-ups** — rename/upload/download/binary-detection are
  done (see above). What's still missing: `ReadFile`/`WriteFile` load the
  whole file into memory both in the daemon and the panel — fine for
  config files, would need streaming for anything large (logs, world
  saves, or an uploaded file bigger than available RAM). No drag-and-drop,
  just a plain file-picker button. No progress indicator on upload/download
  either — fine at config-file sizes, would matter once streaming is added.
- **Databases tab** — the last "not implemented yet" panel. `server_databases`
  table exists, no handler. Needs a decision on how DB credentials
  actually get provisioned (a MySQL/Postgres instance per node? shared?
  the schema has `database_host_id` pointing at a `database_hosts` table
  that was deliberately never created — "out of scope v1" per the
  migration's own note).
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
