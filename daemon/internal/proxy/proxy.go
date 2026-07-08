package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

const sitesAvailable = "/etc/nginx/sites-available"
const sitesEnabled = "/etc/nginx/sites-enabled"

var domainPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)

func ValidDomain(domain string) bool {
	return len(domain) <= 253 && domainPattern.MatchString(domain)
}

func configPath(domain string) string {
	return sitesAvailable + "/powernode-" + domain + ".conf"
}

func enabledPath(domain string) string {
	return sitesEnabled + "/powernode-" + domain + ".conf"
}

func AddDomain(domain string, port int, email string) (string, error) {
	if !ValidDomain(domain) {
		return "", fmt.Errorf("invalid domain name")
	}
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid port")
	}

	conf := fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
`, domain, port)

	if err := os.WriteFile(configPath(domain), []byte(conf), 0644); err != nil {
		return "", fmt.Errorf("write nginx config: %w", err)
	}

	os.Remove(enabledPath(domain))
	if err := os.Symlink(configPath(domain), enabledPath(domain)); err != nil {
		return "", fmt.Errorf("enable nginx site: %w", err)
	}

	if err := reloadNginx(); err != nil {
		return "", err
	}

	return issueCertificate(domain, email), nil
}

func RemoveDomain(domain string) error {
	if !ValidDomain(domain) {
		return fmt.Errorf("invalid domain name")
	}
	os.Remove(enabledPath(domain))
	os.Remove(configPath(domain))
	return reloadNginx()
}

func reloadNginx() error {
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx config test failed: %s", string(out))
	}
	if out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx reload failed: %s", string(out))
	}
	return nil
}

func issueCertificate(domain, email string) string {
	args := []string{"--nginx", "-d", domain, "--non-interactive", "--agree-tos", "--redirect"}
	if email != "" {
		args = append(args, "-m", email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	if err := exec.Command("certbot", args...).Run(); err != nil {
		return "http_only"
	}
	return "active"
}
