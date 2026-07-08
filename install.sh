#!/usr/bin/env bash
set -uo pipefail

REPO_URL="${PANEL_REPO_URL:-https://github.com/superbodik/PowerNode.git}"
REPO_BRANCH="${PANEL_REPO_BRANCH:-main}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ ! -f "${SCRIPT_DIR}/scripts/lib.sh" ]]; then
	CLONE_DIR="/usr/local/src/panel"
	cd / || exit 1
	command -v git >/dev/null 2>&1 || { apt-get update -qq && apt-get install -y -qq git; }
	rm -rf "$CLONE_DIR"
	git clone --depth 1 --branch "$REPO_BRANCH" "$REPO_URL" "$CLONE_DIR"
	exec bash "${CLONE_DIR}/install.sh" "$@"
fi

PROJECT_ROOT="$SCRIPT_DIR"

source "${SCRIPT_DIR}/scripts/lib.sh"
source "${SCRIPT_DIR}/scripts/i18n.sh"
source "${SCRIPT_DIR}/scripts/toolchain.sh"
source "${SCRIPT_DIR}/scripts/docker.sh"
source "${SCRIPT_DIR}/scripts/database.sh"
source "${SCRIPT_DIR}/scripts/domain.sh"
source "${SCRIPT_DIR}/scripts/panel.sh"
source "${SCRIPT_DIR}/scripts/daemon.sh"
source "${SCRIPT_DIR}/scripts/firewall.sh"
source "${SCRIPT_DIR}/scripts/uninstall.sh"
source "${SCRIPT_DIR}/scripts/update.sh"

preflight() {
	log_step "Preflight checks"
	require_root
	require_supported_os
	require_dependencies
	neutralize_policy_rc_d
	apt-get install -y -qq openssl whiptail >/dev/null 2>&1 || true
}

run_master_panel() {
	INSTALL_MODE="panel"
	install_docker
	install_database
	install_panel
	configure_firewall
	log_step "Master panel installed"
}

run_daemon_node() {
	INSTALL_MODE="daemon"
	install_docker
	install_daemon
	configure_firewall
	log_step "Daemon node installed"
}

run_all() {
	INSTALL_MODE="all"
	install_docker
	install_database
	install_panel
	install_daemon
	configure_firewall
	log_step "Master panel + daemon node installed on this single host"
}

run_uninstall() {
	uninstall_panel
	uninstall_daemon
	log_step "Uninstall complete (database, Docker, and secret files were intentionally left in place)"
}

main() {
	if [[ -n "${WINGSD_DAEMON_TOKEN:-}" ]]; then
		preflight
		run_daemon_node
		return
	fi

	if [[ -n "${PANEL_UPDATE:-}" ]]; then
		preflight
		run_update
		return
	fi

	print_banner
	select_language
	preflight

	local choice
	choice=$(ask_menu "$(msg menu_title)" \
		1 "$(msg menu_1)" \
		2 "$(msg menu_2)" \
		3 "$(msg menu_3)" \
		4 "$(msg menu_4)" \
		5 "$(msg menu_5)" \
		6 "$(msg menu_6)")

	case "$choice" in
		1) run_master_panel ;;
		2) run_daemon_node ;;
		3) run_all ;;
		4) run_uninstall ;;
		5) uninstall_full ;;
		6) run_update ;;
		*) die "No option selected, aborting" ;;
	esac
}

main "$@"
