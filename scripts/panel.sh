#!/usr/bin/env bash

PANEL_INSTALL_DIR="/opt/panel"
PANEL_ENV_FILE="/etc/panel/panel.env"
PANEL_SERVICE="/etc/systemd/system/panel.service"

build_panel_binaries() {
	install_go
	install_nodejs

	mkdir -p "$PANEL_INSTALL_DIR"

	local version commit build_date
	version=$(cat "${PROJECT_ROOT}/VERSION" 2>/dev/null || echo "0.0.0-dev")
	commit=$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)
	build_date=$(date -u +%FT%TZ)

	log_step "Building backend"
	(cd "${PROJECT_ROOT}/backend" && go build -ldflags "-X main.version=${version} -X main.commit=${commit} -X main.buildDate=${build_date}" -o "${PANEL_INSTALL_DIR}/panel" ./cmd/panel) \
		|| die "Backend build failed"
	(cd "${PROJECT_ROOT}/backend" && go build -o "${PANEL_INSTALL_DIR}/panel-admin" ./cmd/panel-admin) \
		|| die "panel-admin build failed"
	log_ok "Backend binaries: ${PANEL_INSTALL_DIR}/panel, ${PANEL_INSTALL_DIR}/panel-admin (v${version}, ${commit})"

	log_step "Building frontend"
	(cd "${PROJECT_ROOT}/frontend" && npm ci --silent && npm run build --silent) \
		|| die "Frontend build failed"
	rm -rf "${PANEL_INSTALL_DIR}/public"
	cp -r "${PROJECT_ROOT}/frontend/dist" "${PANEL_INSTALL_DIR}/public"
	log_ok "Frontend assets: ${PANEL_INSTALL_DIR}/public"
}

install_panel() {
	log_step "Installing master panel (backend + frontend)"

	mkdir -p "$PANEL_INSTALL_DIR" /etc/panel

	build_panel_binaries

	write_panel_env
	write_panel_service
	install_nginx_site

	prompt_domain
	apply_domain_to_nginx

	systemctl daemon-reload
	systemctl enable --now panel
	log_ok "panel.service started"

	create_admin_interactive

	echo
	log_step "$(msg admin_created)"
	echo "  $(panel_url)"
	echo
	echo "$(msg daemon_token_intro)"
}

create_admin_interactive() {
	echo
	log_step "$(msg admin_intro)"

	local email username password password_confirm
	read -rp "$(msg admin_ask_email)" email
	read -rp "$(msg admin_ask_username)" username

	while true; do
		read -rsp "$(msg admin_ask_password)" password
		echo
		read -rsp "$(msg admin_ask_password_confirm)" password_confirm
		echo
		if [[ "$password" == "$password_confirm" && ${#password} -ge 8 ]]; then
			break
		fi
		log_warn "$(msg admin_password_mismatch)"
	done

	local db_url
	db_url=$(grep '^PANEL_DATABASE_URL=' "$PANEL_ENV_FILE" | cut -d= -f2-)

	printf '%s\n' "$password" \
		| PANEL_DATABASE_URL="$db_url" "${PANEL_INSTALL_DIR}/panel-admin" -email "$email" -username "$username" \
		|| die "Failed to create admin user"
}

write_panel_env() {
	if [[ -f "$PANEL_ENV_FILE" ]]; then
		log_warn "$PANEL_ENV_FILE already exists — leaving secrets untouched"
		local existing_db_url db_password
		existing_db_url=$(grep '^PANEL_DATABASE_URL=' "$PANEL_ENV_FILE" | cut -d= -f2-)
		db_password=$(echo "$existing_db_url" | sed -E 's#^postgres://[^:]+:([^@]+)@.*#\1#')
		apply_migrations "$db_password"
		return
	fi

	local db_password jwt_secret encryption_key
	db_password=$(random_secret 24)
	jwt_secret=$(random_secret 32)
	encryption_key=$(random_secret 16)

	provision_database "$db_password"

	cat >"$PANEL_ENV_FILE" <<-EOF
	PANEL_HTTP_ADDR=127.0.0.1:8080
	PANEL_DATABASE_URL=postgres://panel:${db_password}@127.0.0.1:5432/panel?sslmode=disable
	PANEL_REDIS_ADDR=127.0.0.1:6379
	PANEL_JWT_SECRET=${jwt_secret}
	PANEL_ENCRYPTION_KEY=${encryption_key}
	PANEL_SOURCE_DIR=${PROJECT_ROOT}
	EOF
	chmod 600 "$PANEL_ENV_FILE"
	log_ok "Wrote $PANEL_ENV_FILE (mode 600)"
}

write_panel_service() {
	cat >"$PANEL_SERVICE" <<-EOF
	[Unit]
	Description=Panel master control-plane
	After=network.target postgresql.service redis-server.service

	[Service]
	Type=simple
	EnvironmentFile=${PANEL_ENV_FILE}
	ExecStart=${PANEL_INSTALL_DIR}/panel
	Restart=on-failure
	RestartSec=2
	User=www-data
	AmbientCapabilities=CAP_NET_BIND_SERVICE

	[Install]
	WantedBy=multi-user.target
	EOF
	log_ok "Wrote $PANEL_SERVICE"
}

install_nginx_site() {
	if ! require_command nginx; then
		apt-get install -y -qq nginx || die "nginx installation failed"
	fi

	cat >/etc/nginx/sites-available/panel <<-'EOF'
	server {
		listen 80;
		server_name _;

		root /opt/panel/public;
		index index.html;

		location /api/ {
			proxy_pass http://127.0.0.1:8080;
			proxy_set_header Host $host;
			proxy_set_header X-Real-IP $remote_addr;
		}

		location /ws/ {
			proxy_pass http://127.0.0.1:8080;
			proxy_http_version 1.1;
			proxy_set_header Upgrade $http_upgrade;
			proxy_set_header Connection "upgrade";
		}

		location / {
			try_files $uri /index.html;
		}
	}
	EOF
	ln -sf /etc/nginx/sites-available/panel /etc/nginx/sites-enabled/panel
	rm -f /etc/nginx/sites-enabled/default
	systemctl reload nginx 2>/dev/null || systemctl restart nginx
	log_ok "nginx site installed (proxies /api and /ws to :8080, serves SPA)"
}
