#!/bin/bash
set -e

if [ "$TURN_ENABLED" = "true" ]; then
    echo "Starting Coturn..."
    mkdir -p /etc/coturn

    # Use hostname as default realm if not provided
    REALM="${TURN_REALM:-$(hostname)}"

    # Generate TURN config
    cat <<EOF > /etc/coturn/turnserver.conf
listening-port=3478
listening-ip=0.0.0.0
fingerprint
lt-cred-mech
realm=$REALM
min-port=49152
max-port=49252
log-file=stdout
verbose
no-cli
no-tls
no-dtls
no-multicast-peers
# Allow private IP ranges for users behind NAT
allowed-peer-ip=10.0.0.0-10.255.255.255
allowed-peer-ip=172.16.0.0-172.31.255.255
allowed-peer-ip=192.168.0.0-192.168.255.255
# Allow all public IPs
allowed-peer-ip=0.0.0.0-255.255.255.255
EOF

    if [ -n "$TURN_EXTERNAL_IP" ]; then
        echo "external-ip=$TURN_EXTERNAL_IP" >> /etc/coturn/turnserver.conf
        echo "relay-ip=$TURN_EXTERNAL_IP" >> /etc/coturn/turnserver.conf
    fi

    if [ -n "$TURN_USERNAME" ] && [ -n "$TURN_PASSWORD" ]; then
        echo "user=$TURN_USERNAME:$TURN_PASSWORD" >> /etc/coturn/turnserver.conf
    fi

    # Start coturn in background
    turnserver -c /etc/coturn/turnserver.conf &
fi

echo "Starting Payambar..."
./payambar
