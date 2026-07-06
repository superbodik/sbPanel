#!/usr/bin/env bash

uninstall_panel() {
	log_step "Removing master panel"
	systemctl disable --now panel 2>/dev/null
	rm -f /etc/systemd/system/panel.service
	rm -rf /opt/panel
	rm -f /etc/nginx/sites-enabled/panel /etc/nginx/sites-available/panel
	systemctl reload nginx 2>/dev/null || true
	systemctl daemon-reload
	log_ok "Panel removed (database and /etc/panel/panel.env kept — delete manually if truly unwanted)"
}

uninstall_daemon() {
	log_step "Removing node daemon"
	systemctl disable --now wingsd 2>/dev/null
	rm -f /etc/systemd/system/wingsd.service
	rm -rf /opt/wingsd
	systemctl daemon-reload
	log_ok "wingsd removed (/var/lib/wingsd/servers and /etc/wingsd/wingsd.env kept)"
}

uninstall_full() {
	echo
	log_warn "$(msg full_uninstall_warn)"
	local confirm
	read -rp "$(msg full_uninstall_confirm)" confirm
	if [[ "$confirm" != "DELETE" ]]; then
		log_ok "$(msg full_uninstall_aborted)"
		return
	fi

	uninstall_panel
	uninstall_daemon

	log_step "Removing database"
	if require_command psql; then
		sudo -u postgres psql -c "DROP DATABASE IF EXISTS panel;" 2>/dev/null
		sudo -u postgres psql -c "DROP ROLE IF EXISTS panel;" 2>/dev/null
	fi

	log_step "Removing certificates"
	local fqdn
	fqdn=$(grep -oP 'server_name\s+\K[^;_]+' /etc/nginx/sites-available/panel 2>/dev/null | tr -d ' ')
	if [[ -n "$fqdn" ]] && require_command certbot; then
		certbot delete --cert-name "$fqdn" --non-interactive 2>/dev/null
	fi
	rm -f /etc/nginx/sites-enabled/panel /etc/nginx/sites-available/panel

	log_step "Purging Docker, PostgreSQL, Redis, nginx packages and their data"
	systemctl stop docker postgresql redis-server nginx 2>/dev/null
	local pg_packages
	pg_packages=$(dpkg-query -W -f '${Package}\n' 'postgresql*' 2>/dev/null)
	apt-get purge -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin \
		${pg_packages} redis-server nginx nginx-common certbot python3-certbot-nginx 2>/dev/null
	apt-get autoremove -y -qq 2>/dev/null
	rm -rf /var/lib/docker /var/lib/containerd /var/lib/postgresql /var/lib/redis \
		/var/lib/wingsd /etc/panel /etc/wingsd /etc/nginx/sites-available/panel \
		/etc/postgresql /etc/postgresql-common /var/log/postgresql

	log_step "Resetting firewall"
	require_command ufw && ufw --force reset >/dev/null 2>&1

	log_ok "$(msg full_uninstall_done)"
}
