#!/bin/sh
set -eu

IFACE="${IFACE:-enp0s1}"
IP_ADDR="${IP_ADDR:-192.168.7.2/24}"
GATEWAY="${GATEWAY:-192.168.7.1}"
DNS_SERVER="${DNS_SERVER:-192.168.7.1}"
NETWORK_FILE="${NETWORK_FILE:-/etc/systemd/network/10-eth-static.network}"
SWAP_FILE="${SWAP_FILE:-/swapfile}"
SWAP_SIZE_MB="${SWAP_SIZE_MB:-1024}"
ENABLE_SWAP="${ENABLE_SWAP:-false}"
DISABLE_ROOT_EXPIRY="${DISABLE_ROOT_EXPIRY:-false}"

if [ "$(id -u)" -ne 0 ]; then
    echo "This script must be run as root." >&2
    exit 1
fi

backup_file() {
    path="$1"
    if [ -f "$path" ]; then
        cp "$path" "${path}.bak.$(date +%Y%m%d%H%M%S)"
    fi
}

write_network_config() {
    mkdir -p "$(dirname "$NETWORK_FILE")"
    backup_file "$NETWORK_FILE"
    cat >"$NETWORK_FILE" <<EOF
[Match]
Name=$IFACE

[Network]
Address=$IP_ADDR
Gateway=$GATEWAY
DNS=$DNS_SERVER
DHCP=no
LinkLocalAddressing=no
LLMNR=no
MulticastDNS=no
IPv6AcceptRA=no
EOF
}

configure_network_services() {
    if systemctl list-unit-files | grep -q '^dhcpcd\.service'; then
        systemctl disable --now dhcpcd || true
    fi
    systemctl enable --now systemd-networkd
    systemctl restart systemd-networkd
    sleep 3
}

ensure_swap() {
    current_size_bytes=0
    desired_size_bytes=$((SWAP_SIZE_MB * 1024 * 1024))

    if [ -f "$SWAP_FILE" ]; then
        current_size_bytes=$(wc -c <"$SWAP_FILE")
    fi

    if [ "$current_size_bytes" -ne "$desired_size_bytes" ]; then
        swapoff "$SWAP_FILE" 2>/dev/null || true
        rm -f "$SWAP_FILE"
        if fallocate -l "${SWAP_SIZE_MB}M" "$SWAP_FILE" 2>/dev/null; then
            :
        else
            dd if=/dev/zero of="$SWAP_FILE" bs=1M count="$SWAP_SIZE_MB" status=progress
        fi
        chmod 600 "$SWAP_FILE"
        mkswap "$SWAP_FILE"
    fi

    swapon "$SWAP_FILE"

    if ! grep -q "^$SWAP_FILE " /etc/fstab 2>/dev/null; then
        printf '%s\n' "$SWAP_FILE none swap sw 0 0" >> /etc/fstab
    fi
}

disable_root_password_expiry() {
    if [ "$DISABLE_ROOT_EXPIRY" = "true" ]; then
        chage -I -1 -m 0 -M 99999 -E -1 root
        passwd -x -1 root
    fi
}

print_status() {
    echo "=== Network ==="
    ip -br addr show dev "$IFACE" || true
    ip route || true
    echo "=== Services ==="
    systemctl is-active systemd-networkd dhcpcd 2>/dev/null || true
    echo "=== Swap ==="
    swapon --show 2>/dev/null || true
    free -m || true
}

write_network_config
configure_network_services
if [ "$ENABLE_SWAP" = "true" ]; then
    ensure_swap
fi
disable_root_password_expiry
print_status
