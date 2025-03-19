if [ "$1" != "1" ]; then
    if command -v systemctl >/dev/null 2>&1; then
        systemctl stop otelcol-google.service
        systemctl disable otelcol-google.service
    fi
fi