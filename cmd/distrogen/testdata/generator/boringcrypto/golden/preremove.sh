if [ "$1" != "1" ]; then
    if command -v systemctl >/dev/null 2>&1; then
        systemctl stop otelcol-basic.service
        systemctl disable otelcol-basic.service
    fi
fi
