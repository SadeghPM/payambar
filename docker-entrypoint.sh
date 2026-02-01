#!/bin/bash
set -e

if [ "$TURN_ENABLED" = "true" ]; then
    echo "Starting Coturn..."

    # Generate TURN config
    cat <<EOF > /etc/coturn/turnserver.conf
listening-port=3478
fingerprint
lt-cred-mech
EOF

    if [ -n "$TURN_REALM" ]; then
        echo "realm=$TURN_REALM" >> /etc/coturn/turnserver.conf
    fi

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
