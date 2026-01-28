# Keykammer Systemd Service

This document explains how to run Keykammer discovery server as a Linux systemd service.

## Quick Installation

```bash
# Build keykammer
make build

# Install and configure service (requires root)
sudo ./install-discovery-service.sh

# Start the service
sudo systemctl start keykammer-discovery

# Check status
sudo systemctl status keykammer-discovery
```

## Manual Installation

If you prefer to install manually:

### 1. Create User

```bash
sudo useradd --system --home-dir /var/lib/keykammer --shell /bin/false keykammer
```

### 2. Install Binary

```bash
sudo cp keykammer /usr/local/bin/keykammer
sudo chown root:root /usr/local/bin/keykammer
sudo chmod 755 /usr/local/bin/keykammer
```

### 3. Create Data Directory

```bash
sudo mkdir -p /var/lib/keykammer
sudo chown keykammer:keykammer /var/lib/keykammer
sudo chmod 750 /var/lib/keykammer
```

### 4. Install Service File

```bash
sudo cp keykammer-discovery.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable keykammer-discovery
```

## Service Management

### Basic Commands

```bash
# Start service
sudo systemctl start keykammer-discovery

# Stop service
sudo systemctl stop keykammer-discovery

# Restart service
sudo systemctl restart keykammer-discovery

# Check status
sudo systemctl status keykammer-discovery

# Enable auto-start on boot
sudo systemctl enable keykammer-discovery

# Disable auto-start
sudo systemctl disable keykammer-discovery
```

### Monitoring

```bash
# View logs (follow mode)
sudo journalctl -u keykammer-discovery -f

# View recent logs
sudo journalctl -u keykammer-discovery --since "1 hour ago"

# View logs with timestamps
sudo journalctl -u keykammer-discovery -o cat
```

## Configuration

The service runs with these settings:

- **User**: `keykammer` (non-privileged system user)
- **Port**: `53952` (default Keykammer port)
- **Working Directory**: `/var/lib/keykammer`
- **Auto-restart**: Enabled with 5-second delay
- **Security**: Hardened with various systemd security features

### Security Features

The service includes several security hardening options:

- `NoNewPrivileges=yes` - Prevents privilege escalation
- `PrivateTmp=yes` - Private /tmp directory
- `ProtectSystem=strict` - Read-only filesystem except allowed paths
- `ProtectHome=yes` - No access to user home directories

### Resource Limits

- **File Descriptors**: 65,536 (for handling many connections)
- **Processes**: 4,096 (reasonable limit for discovery server)

## Firewall Configuration

Make sure to open the discovery server port:

```bash
# UFW (Ubuntu/Debian)
sudo ufw allow 53952/tcp

# firewalld (RHEL/CentOS/Fedora)
sudo firewall-cmd --permanent --add-port=53952/tcp
sudo firewall-cmd --reload

# iptables (manual)
sudo iptables -A INPUT -p tcp --dport 53952 -j ACCEPT
```

## Health Checking

The discovery server provides a health endpoint:

```bash
# Check if server is responding
curl http://localhost:53952/health

# Expected response: {"status":"ok"}
```

You can use this for monitoring systems like Nagios, Zabbix, or simple cron jobs.

## Troubleshooting

### Service Won't Start

Check logs for errors:
```bash
sudo journalctl -u keykammer-discovery --no-pager
```

Common issues:
- Binary not found: Check `/usr/local/bin/keykammer` exists and is executable
- Permission denied: Ensure `keykammer` user has access to data directory
- Port in use: Check if another service is using port 53952

### High Resource Usage

Monitor resource usage:
```bash
# CPU and memory usage
sudo systemctl status keykammer-discovery

# Detailed process info
sudo ps aux | grep keykammer
```

### Log Rotation

Systemd automatically handles log rotation through journald. To configure retention:

```bash
# Edit journald config
sudo nano /etc/systemd/journald.conf

# Example settings:
# SystemMaxUse=100M
# SystemMaxFiles=10
```

## Uninstallation

To remove the service:

```bash
# Stop and disable service
sudo systemctl stop keykammer-discovery
sudo systemctl disable keykammer-discovery

# Remove service file
sudo rm /etc/systemd/system/keykammer-discovery.service
sudo systemctl daemon-reload

# Remove binary
sudo rm /usr/local/bin/keykammer

# Remove data directory (optional)
sudo rm -rf /var/lib/keykammer

# Remove user (optional)
sudo userdel keykammer
```

## Production Recommendations

1. **Monitoring**: Set up monitoring for the service and health endpoint
2. **Backups**: No data to backup (discovery server is stateless)
3. **Updates**: Stop service, replace binary, restart service
4. **Logs**: Configure log retention based on your requirements
5. **Security**: Keep the system updated and consider running behind a reverse proxy