#!/usr/bin/env bash
# One-time setup of a fresh Ubuntu 24.04 server for Wera. Run as root:
#
#   curl -fsSL https://raw.githubusercontent.com/wera-jobs/Wera/main/deploy/hetzner/setup-server.sh | bash
#
# It installs Docker, opens only SSH/HTTP/HTTPS in the firewall, turns on
# automatic security updates, adds swap, creates a "wera" user (with
# root's SSH keys), turns off SSH password and root logins, downloads the
# deploy files to /home/wera/wera, and schedules a nightly backup.
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

RAW=https://raw.githubusercontent.com/wera-jobs/Wera/main/deploy/hetzner
APP_USER=wera
APP_DIR=/home/$APP_USER/wera

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get -y upgrade
apt-get -y install ca-certificates curl ufw unattended-upgrades fail2ban openssl

if ! command -v docker >/dev/null; then
	curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker

# Firewall. Docker-published ports skip ufw, which is why compose.yml
# publishes only Caddy's 80/443 and binds the admin console to 127.0.0.1.
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 443/udp
ufw --force enable

dpkg-reconfigure -f noninteractive unattended-upgrades

if ! swapon --show | grep -q .; then
	fallocate -l 2G /swapfile
	chmod 600 /swapfile
	mkswap /swapfile
	swapon /swapfile
	echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi

if ! id "$APP_USER" >/dev/null 2>&1; then
	adduser --disabled-password --gecos "" "$APP_USER"
fi
usermod -aG docker "$APP_USER"
if [[ -s /root/.ssh/authorized_keys ]]; then
	install -d -m 700 -o "$APP_USER" -g "$APP_USER" "/home/$APP_USER/.ssh"
	install -m 600 -o "$APP_USER" -g "$APP_USER" /root/.ssh/authorized_keys "/home/$APP_USER/.ssh/authorized_keys"
fi

# Lock SSH down only once the wera user can log in with a key.
if [[ -s /home/$APP_USER/.ssh/authorized_keys ]]; then
	cat >/etc/ssh/sshd_config.d/10-wera.conf <<'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
EOF
	systemctl reload ssh
	ssh_note="SSH: log in as $APP_USER with your key (root and passwords are off)."
else
	ssh_note="WARNING: root has no SSH key, so SSH password login was left on. Add a key, then rerun."
fi

install -d -o "$APP_USER" -g "$APP_USER" "$APP_DIR"
for f in compose.yml Caddyfile .env.example deploy.sh README.md; do
	[[ -f "$APP_DIR/$f" ]] || curl -fsSL "$RAW/$f" -o "$APP_DIR/$f"
done
chmod +x "$APP_DIR/deploy.sh"
chown -R "$APP_USER:$APP_USER" "$APP_DIR"

# Nightly database backup at 03:30 server time.
echo "30 3 * * * $APP_DIR/deploy.sh backup >>$APP_DIR/backups.log 2>&1" | crontab -u "$APP_USER" -

cat <<EOF

Server ready. $ssh_note
Next:
  ssh $APP_USER@<this server's IP>
  cd ~/wera && ./deploy.sh init && nano .env && ./deploy.sh up
EOF
