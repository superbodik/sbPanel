#!/usr/bin/env bash

declare -A MSG_EN
declare -A MSG_RU

INSTALL_LANG="en"

msg() {
	local key="$1"
	if [[ "$INSTALL_LANG" == "ru" ]]; then
		printf '%s' "${MSG_RU[$key]:-${MSG_EN[$key]:-$key}}"
	else
		printf '%s' "${MSG_EN[$key]:-$key}"
	fi
}

select_language() {
	local choice
	choice=$(ask_menu "Language / Язык" \
		1 "English" \
		2 "Русский (подробные пояснения на каждом шаге)")
	case "$choice" in
		2) INSTALL_LANG="ru" ;;
		*) INSTALL_LANG="en" ;;
	esac
}

MSG_EN[menu_title]="PowerNode Installer"
MSG_RU[menu_title]="Установщик PowerNode"

MSG_EN[menu_1]="Install Master Panel (Backend + Frontend + DB)"
MSG_RU[menu_1]="Установить Мастер-панель (Backend + Frontend + БД)"

MSG_EN[menu_2]="Install Daemon Node (Wings + Docker)"
MSG_RU[menu_2]="Установить Демон-ноду (Wings + Docker)"

MSG_EN[menu_3]="Install everything (single host)"
MSG_RU[menu_3]="Установить всё вместе (на одном сервере)"

MSG_EN[menu_4]="Uninstall (keeps database and data)"
MSG_RU[menu_4]="Удалить (база данных и файлы серверов сохраняются)"

MSG_EN[menu_5]="FULL removal — deletes database, Docker, all data (DANGEROUS)"
MSG_RU[menu_5]="ПОЛНОЕ удаление — сотрёт базу данных, Docker и все данные (ОПАСНО)"

MSG_EN[menu_6]="Check for updates / update this installation"
MSG_RU[menu_6]="Проверить обновления / обновить установку"

MSG_EN[domain_intro]="The panel needs a domain name to issue a free HTTPS certificate (Let's Encrypt) for. Make sure a DNS A-record for that domain/subdomain already points at this server's public IP before continuing — the certificate request will fail otherwise."
MSG_RU[domain_intro]="Панели нужно доменное имя, чтобы автоматически выпустить бесплатный HTTPS-сертификат (Let's Encrypt). Перед тем как продолжить, убедитесь, что DNS A-запись для этого домена/поддомена уже указывает на публичный IP этого сервера — иначе выпуск сертификата не пройдёт."

MSG_EN[domain_ask_subdomain]="Subdomain (e.g. panel, leave empty for none): "
MSG_RU[domain_ask_subdomain]="Поддомен (например panel, оставьте пустым если не нужен): "

MSG_EN[domain_ask_root]="Root domain (e.g. example.com): "
MSG_RU[domain_ask_root]="Основной домен (например example.com): "

MSG_EN[domain_skip]="No domain entered — the panel will stay on plain HTTP, reachable by this server's IP address only. You can rerun install.sh later to add a domain and HTTPS."
MSG_RU[domain_skip]="Домен не введён — панель останется на обычном HTTP и будет доступна только по IP-адресу сервера. Вы можете повторно запустить install.sh позже, чтобы добавить домен и HTTPS."

MSG_EN[cert_issuing]="Requesting a Let's Encrypt certificate for"
MSG_RU[cert_issuing]="Запрашиваем сертификат Let's Encrypt для"

MSG_EN[cert_failed]="Certificate request failed — check that DNS for this domain already points at this server, then rerun install.sh. Continuing on plain HTTP for now."
MSG_RU[cert_failed]="Не удалось получить сертификат — проверьте, что DNS-запись домена уже указывает на этот сервер, затем повторно запустите install.sh. Пока продолжаем на обычном HTTP."

MSG_EN[cert_email_ask]="Email for Let's Encrypt renewal notices: "
MSG_RU[cert_email_ask]="Email для уведомлений Let's Encrypt о продлении сертификата: "

MSG_EN[admin_intro]="No users exist yet. Let's create the main admin account you'll use to log in to the panel."
MSG_RU[admin_intro]="Пользователей пока нет. Сейчас создадим главную учётную запись администратора, под которой вы будете заходить в панель."

MSG_EN[admin_ask_email]="Admin email: "
MSG_RU[admin_ask_email]="Email администратора: "

MSG_EN[admin_ask_username]="Admin username: "
MSG_RU[admin_ask_username]="Имя пользователя администратора: "

MSG_EN[admin_ask_password]="Admin password (min 8 characters): "
MSG_RU[admin_ask_password]="Пароль администратора (минимум 8 символов): "

MSG_EN[admin_ask_password_confirm]="Confirm password: "
MSG_RU[admin_ask_password_confirm]="Повторите пароль: "

MSG_EN[admin_password_mismatch]="Passwords did not match — try again."
MSG_RU[admin_password_mismatch]="Пароли не совпадают — попробуйте снова."

MSG_EN[admin_created]="Admin account ready. Log in at the address below."
MSG_RU[admin_created]="Учётная запись администратора готова. Войдите по адресу ниже."

MSG_EN[daemon_token_intro]="Now log in to the panel in your browser, open the 'Nodes' page, and click 'Add node' to register this machine. The panel will show you a one-time daemon token — paste it below."
MSG_RU[daemon_token_intro]="Теперь зайдите в панель через браузер, откройте страницу 'Nodes' и нажмите 'Add node', чтобы зарегистрировать эту машину. Панель покажет одноразовый токен демона — вставьте его ниже."

MSG_EN[daemon_token_ask]="Daemon token from the panel: "
MSG_RU[daemon_token_ask]="Токен демона из панели: "

MSG_EN[panel_url_ask]="Panel URL (e.g. https://panel.example.com — leave blank to configure SFTP later): "
MSG_RU[panel_url_ask]="Адрес панели (например https://panel.example.com — можно оставить пустым и настроить SFTP позже): "

MSG_EN[full_uninstall_warn]="This PERMANENTLY deletes the panel database, all servers' data directories, Docker and everything running in it, and every generated secret. There is no undo."
MSG_RU[full_uninstall_warn]="Это НАВСЕГДА удалит базу данных панели, все каталоги данных серверов, Docker и всё, что в нём запущено, а также все сгенерированные секреты. Отменить это будет нельзя."

MSG_EN[full_uninstall_confirm]="Type DELETE (in capitals) to confirm, anything else cancels: "
MSG_RU[full_uninstall_confirm]="Введите DELETE (заглавными буквами) для подтверждения, любой другой ввод отменит удаление: "

MSG_EN[full_uninstall_aborted]="Aborted — nothing was deleted."
MSG_RU[full_uninstall_aborted]="Отменено — ничего не удалено."

MSG_EN[full_uninstall_done]="Full removal complete."
MSG_RU[full_uninstall_done]="Полное удаление завершено."
