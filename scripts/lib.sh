#!/usr/bin/env bash
set -uo pipefail

COLOR_GREEN='\033[0;32m'
COLOR_RED='\033[0;31m'
COLOR_YELLOW='\033[0;33m'
COLOR_RESET='\033[0m'

log_ok()   { printf "${COLOR_GREEN}[ OK ]${COLOR_RESET} %s\n" "$1" >&2; }
log_err()  { printf "${COLOR_RED}[ ERR ]${COLOR_RESET} %s\n" "$1" >&2; }
log_warn() { printf "${COLOR_YELLOW}[ WARN ]${COLOR_RESET} %s\n" "$1" >&2; }
log_step() { printf "\n${COLOR_YELLOW}==>${COLOR_RESET} %s\n" "$1" >&2; }

print_banner() {
	printf "\n${COLOR_YELLOW}PowerNode${COLOR_RESET} — self-hosted game server panel\n\n" >&2
}

die() {
	log_err "$1"
	exit 1
}

require_root() {
	if [[ $EUID -ne 0 ]]; then
		die "This script must be run as root (try: sudo ./install.sh)"
	fi
	log_ok "Running as root"
}

require_supported_os() {
	if [[ ! -r /etc/os-release ]]; then
		die "Cannot detect OS: /etc/os-release not found"
	fi
	source /etc/os-release
	case "${ID:-}-${VERSION_ID:-}" in
		ubuntu-24.04|ubuntu-22.04)
			log_ok "Detected supported OS: ${PRETTY_NAME:-$ID $VERSION_ID}"
			;;
		*)
			die "Unsupported OS: ${PRETTY_NAME:-unknown}. This installer supports Ubuntu 22.04/24.04 only."
			;;
	esac
}

require_command() {
	command -v "$1" >/dev/null 2>&1
}

neutralize_policy_rc_d() {
	local policy=/usr/sbin/policy-rc.d
	local backup="${policy}.disabled-by-panel-installer"
	if [[ -e "$policy" && ! -e "$backup" ]]; then
		log_warn "Found ${policy} (blocks service auto-start, common on VPS images built from container templates) — neutralizing it"
		mv "$policy" "$backup"
	fi
	cat >"$policy" <<-'EOF'
	#!/bin/sh
	exit 0
	EOF
	chmod +x "$policy"
}

require_dependencies() {
	local missing=()
	for cmd in curl wget git tar; do
		require_command "$cmd" || missing+=("$cmd")
	done

	if [[ ${#missing[@]} -gt 0 ]]; then
		log_warn "Missing dependencies: ${missing[*]} — installing"
		apt-get update -qq
		apt-get install -y -qq "${missing[@]}" || die "Failed to install: ${missing[*]}"
	fi
	log_ok "All base dependencies present (curl, wget, git, tar)"
}

random_secret() {
	local bytes="${1:-32}"
	openssl rand -hex "$bytes"
}

ask_menu() {
	local title="$1"
	shift
	if require_command whiptail; then
		whiptail --title "$title" --menu "Choose an option:" 16 70 6 "$@" 3>&1 1>&2 2>&3
		return
	fi

	log_warn "whiptail not found, falling back to plain-text menu"
	echo "$title" >&2
	while [[ $# -gt 0 ]]; do
		echo "  [$1] $2" >&2
		shift 2
	done
	read -rp "Enter choice: " choice
	echo "$choice"
}
