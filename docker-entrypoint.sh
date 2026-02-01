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
fingerprint
lt-cred-mech
realm=$REALM
min-port=49152
max-port=49252
log-file=stdout
EOF

    if [ -n "$TURN_EXTERNAL_IP" ]; then
        echo "external-ip=$TURN_EXTERNAL_IP" >> /etc/coturn/turnserver.conf
    fi

    if [ -n "$TURN_USERNAME" ] && [ -n "$TURN_PASSWORD" ]; then
        echo "user=$TURN_USERNAME:$TURN_PASSWORD" >> /etc/coturn/turnserver.conf
    fi

    # Start coturn in background
    turnserver -c /etc/coturn/turnserver.conf &
fi

echo "Starting Payambar..."
./payambar
