#!/usr/bin/env bash
set -euo pipefail

# Deployment script for Mezon Fund Bot on Ubuntu
INSTALL_DIR="/opt/fundbot"
SERVICE_NAME="fundbot.service"
USER_NAME="${SUDO_USER:-$(whoami)}"

echo "=== Deploying Mezon Fund Bot ==="
echo "Target directory: ${INSTALL_DIR}"
echo "Running user: ${USER_NAME}"

# 1. Create target directory
sudo mkdir -p "${INSTALL_DIR}"

# 2. Copy binary and assets
if [ -f "bin/fundbot-linux-amd64" ]; then
    echo "Installing pre-built Linux binary..."
    sudo install -m 755 "bin/fundbot-linux-amd64" "${INSTALL_DIR}/fundbot"
elif [ -f "bin/fundbot" ]; then
    echo "Installing bin/fundbot..."
    sudo install -m 755 "bin/fundbot" "${INSTALL_DIR}/fundbot"
else
    echo "Building binary locally..."
    go build -o bin/fundbot ./cmd/fundbot
    sudo install -m 755 bin/fundbot "${INSTALL_DIR}/fundbot"
fi

# Copy wallets mapping if present
if [ -f "wallets.json" ]; then
    sudo cp "wallets.json" "${INSTALL_DIR}/wallets.json"
fi

# Copy .env if not exists in target
if [ ! -f "${INSTALL_DIR}/.env" ]; then
    if [ -f ".env" ]; then
        sudo cp ".env" "${INSTALL_DIR}/.env"
        sudo chmod 600 "${INSTALL_DIR}/.env"
        echo "Copied .env configuration."
    elif [ -f ".env.example" ]; then
        sudo cp ".env.example" "${INSTALL_DIR}/.env"
        sudo chmod 600 "${INSTALL_DIR}/.env"
        echo "Created .env from .env.example - please edit ${INSTALL_DIR}/.env"
    fi
fi

# Fix ownership
sudo chown -R "${USER_NAME}:${USER_NAME}" "${INSTALL_DIR}"

# 3. Install systemd service
echo "Installing systemd service..."
sudo sed "s/User=ubuntu/User=${USER_NAME}/g; s/Group=ubuntu/Group=${USER_NAME}/g" deploy/fundbot.service | sudo tee "/etc/systemd/system/${SERVICE_NAME}" > /dev/null

sudo systemctl daemon-reload
sudo systemctl enable "${SERVICE_NAME}"
sudo systemctl restart "${SERVICE_NAME}"

echo "=== Deployment Complete ==="
echo "Check status: sudo systemctl status ${SERVICE_NAME}"
echo "View logs:    journalctl -u ${SERVICE_NAME} -f"
