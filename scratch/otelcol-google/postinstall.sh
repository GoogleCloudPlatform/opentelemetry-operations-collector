if command -v systemctl >/dev/null 2>&1; then
    if [ -d /run/systemd/system ]; then
        systemctl daemon-reload
    fi
    systemctl enable otelcol-google.service
    if [ -f /etc/otelcol-google/config.yaml ]; then
        if [ -d /run/systemd/system ]; then
            systemctl restart otelcol-google.service
        fi
    fi
fi
