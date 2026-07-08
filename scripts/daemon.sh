#!/usr/bin/env bash

DAEMON_INSTALL_DIR="/opt/wingsd"
DAEMON_ENV_FILE="/etc/wingsd/wingsd.env"
DAEMON_SERVICE="/etc/systemd/system/wingsd.service"
DAEMON_DATA_DIR="/var/lib/wingsd/servers"

build_daemon_binary() {
	install_go
	require_command docker || die "Docker must be installed before wingsd (this node runs containers directly)"

	mkdir -p "$DAEMON_INSTALL_DIR" "$DAEMON_DATA_DIR"

	local version
	version=$(cat "${PROJECT_ROOT}/VERSION" 2>/dev/null || echo "0.0.0-dev")

	log_step "Building daemon"
	(cd "${PROJECT_ROOT}/daemon" && go build -ldflags "-X main.version=${version}" -o "${DAEMON_INSTALL_DIR}/wingsd" ./cmd/wingsd) \
		|| die "Daemon build failed"
	log_ok "Daemon binary: ${DAEMON_INSTALL_DIR}/wingsd (v${version})"
}

install_daemon() {
	log_step "Installing node daemon (wingsd)"

	mkdir -p /etc/wingsd

	build_daemon_binary
	install_daemon_proxy

	write_daemon_env
	write_daemon_service

	systemctl daemon-reload
	systemctl enable wingsd
	systemctl restart wingsd
	log_ok "wingsd.service (re)started"
	log_ok "SFTP ready on port 2022 — users connect with <username>.<server-id>, using an SSH key added on the Account page"
}

install_daemon_proxy() {
	if ! require_command nginx; then
		apt-get install -y -qq nginx || die "nginx installation failed (required for the custom-domains feature)"
	fi
	systemctl enable --now nginx 2>/dev/null
	if ! require_command certbot; then
		apt-get install -y -qq certbot python3-certbot-nginx \
			|| log_warn "certbot installation failed — custom domains will stay on plain HTTP until it's installed manually"
	fi
	log_ok "nginx + certbot ready (used for per-server custom domains)"
}

resolve_panel_url() {
	local panel_url="${WINGSD_PANEL_URL:-}"
	if [[ -n "$panel_url" ]]; then
		echo "$panel_url"
		return
	fi

	if [[ "${INSTALL_MODE:-}" == "all" ]] || systemctl is-active --quiet panel 2>/dev/null; then
		echo "http://127.0.0.1:8080"
		return
	fi

	echo >&2
	echo "$(msg panel_url_intro)" >&2
	while [[ -z "$panel_url" ]]; do
		read -rp "$(msg panel_url_ask)" panel_url
		if [[ -z "$panel_url" ]]; then
			log_warn "$(msg panel_url_required)"
		fi
	done
	echo "$panel_url"
}

write_daemon_env() {
	if [[ -f "$DAEMON_ENV_FILE" ]]; then
		if [[ -n "${WINGSD_DAEMON_TOKEN:-}" ]]; then
			sed -i "s|^WINGSD_DAEMON_TOKEN=.*|WINGSD_DAEMON_TOKEN=${WINGSD_DAEMON_TOKEN}|" "$DAEMON_ENV_FILE"
			log_ok "Updated daemon token in $DAEMON_ENV_FILE"
		else
			log_warn "$DAEMON_ENV_FILE already exists — leaving it untouched (no WINGSD_DAEMON_TOKEN provided to update it)"
		fi

		if grep -q '^WINGSD_PANEL_URL=.\+' "$DAEMON_ENV_FILE"; then
			if [[ -n "${WINGSD_PANEL_URL:-}" ]]; then
				sed -i "s|^WINGSD_PANEL_URL=.*|WINGSD_PANEL_URL=${WINGSD_PANEL_URL}|" "$DAEMON_ENV_FILE"
				log_ok "Updated panel URL in $DAEMON_ENV_FILE"
			fi
		else
			local panel_url
			panel_url=$(resolve_panel_url)
			if grep -q '^WINGSD_PANEL_URL=' "$DAEMON_ENV_FILE"; then
				sed -i "s|^WINGSD_PANEL_URL=.*|WINGSD_PANEL_URL=${panel_url}|" "$DAEMON_ENV_FILE"
			else
				echo "WINGSD_PANEL_URL=${panel_url}" >> "$DAEMON_ENV_FILE"
			fi
			log_ok "Set panel URL in $DAEMON_ENV_FILE (was missing — this is required for SFTP logins to work)"
		fi
		return
	fi

	local node_uuid daemon_token panel_url
	node_uuid=$(cat /proc/sys/kernel/random/uuid)

	if [[ -n "${WINGSD_DAEMON_TOKEN:-}" ]]; then
		daemon_token="$WINGSD_DAEMON_TOKEN"
		log_ok "Using daemon token from WINGSD_DAEMON_TOKEN"
	else
		echo
		echo "$(msg daemon_token_intro)"
		read -rp "$(msg daemon_token_ask)" daemon_token
		if [[ -z "$daemon_token" ]]; then
			die "A daemon token is required — create the node in the panel first (Nodes -> Add node)"
		fi
	fi

	panel_url=$(resolve_panel_url)

	cat >"$DAEMON_ENV_FILE" <<-EOF
	WINGSD_NODE_UUID=${node_uuid}
	WINGSD_DAEMON_TOKEN=${daemon_token}
	WINGSD_PANEL_URL=${panel_url}
	WINGSD_HTTP_ADDR=0.0.0.0:8443
	WINGSD_DATA_DIR=${DAEMON_DATA_DIR}
	EOF
	chmod 600 "$DAEMON_ENV_FILE"
	log_ok "Wrote $DAEMON_ENV_FILE (mode 600), panel URL: ${panel_url}"
	log_warn "Running without TLS certs configured — set WINGSD_TLS_CERT/WINGSD_TLS_KEY in $DAEMON_ENV_FILE for production"
}

write_daemon_service() {
	cat >"$DAEMON_SERVICE" <<-EOF
	[Unit]
	Description=wingsd node daemon
	After=network.target docker.service
	Requires=docker.service

	[Service]
	Type=simple
	EnvironmentFile=${DAEMON_ENV_FILE}
	ExecStart=${DAEMON_INSTALL_DIR}/wingsd
	Restart=on-failure
	RestartSec=2
	User=root

	[Install]
	WantedBy=multi-user.target
	EOF
	log_ok "Wrote $DAEMON_SERVICE"
}
