#!/bin/bash

# Keykammer Discovery Server Installation Script
# Installs and configures systemd service for Keykammer discovery server

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BINARY_PATH="/usr/local/bin/keykammer"
SERVICE_FILE="/etc/systemd/system/keykammer-discovery.service"
DATA_DIR="/var/lib/keykammer"
USER="keykammer"
GROUP="keykammer"

echo -e "${GREEN}Keykammer Discovery Server Installation${NC}"
echo "========================================"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root${NC}"
    echo "Usage: sudo ./install-discovery-service.sh"
    exit 1
fi

# Check if keykammer binary exists
if [ ! -f "./keykammer" ]; then
    echo -e "${RED}Error: keykammer binary not found in current directory${NC}"
    echo "Please build keykammer first with: make build"
    exit 1
fi

echo -e "${YELLOW}1. Creating keykammer user and group...${NC}"
if ! id "$USER" &>/dev/null; then
    useradd --system --home-dir "$DATA_DIR" --shell /bin/false "$USER"
    echo "Created user: $USER"
else
    echo "User $USER already exists"
fi

echo -e "${YELLOW}2. Installing keykammer binary...${NC}"
cp ./keykammer "$BINARY_PATH"
chown root:root "$BINARY_PATH"
chmod 755 "$BINARY_PATH"
echo "Installed to: $BINARY_PATH"

echo -e "${YELLOW}3. Creating data directory...${NC}"
mkdir -p "$DATA_DIR"
chown "$USER:$GROUP" "$DATA_DIR"
chmod 750 "$DATA_DIR"
echo "Created: $DATA_DIR"

echo -e "${YELLOW}4. Installing systemd service...${NC}"
cp ./keykammer-discovery.service "$SERVICE_FILE"
chown root:root "$SERVICE_FILE"
chmod 644 "$SERVICE_FILE"
echo "Installed: $SERVICE_FILE"

echo -e "${YELLOW}5. Reloading systemd...${NC}"
systemctl daemon-reload

echo -e "${YELLOW}6. Enabling service...${NC}"
systemctl enable keykammer-discovery.service

echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo "Service commands:"
echo "  Start:   sudo systemctl start keykammer-discovery"
echo "  Stop:    sudo systemctl stop keykammer-discovery"
echo "  Status:  sudo systemctl status keykammer-discovery"
echo "  Logs:    sudo journalctl -u keykammer-discovery -f"
echo ""
echo "The discovery server will run on port 53952"
echo "Make sure to open this port in your firewall:"
echo "  sudo ufw allow 53952/tcp"
echo ""
echo -e "${YELLOW}Start the service now? (y/N)${NC}"
read -r response
if [[ "$response" =~ ^[Yy]$ ]]; then
    systemctl start keykammer-discovery.service
    echo -e "${GREEN}Service started!${NC}"
    systemctl status keykammer-discovery.service --no-pager
fi