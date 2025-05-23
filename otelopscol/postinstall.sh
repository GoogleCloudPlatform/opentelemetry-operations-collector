if command -v systemctl >/dev/null 2>&1; then
    if [ -d /run/systemd/system ]; then
        systemctl daemon-reload
    fi
    systemctl enable otelopscol.service
    if [ -f /etc/otelopscol/config.yaml ]; then
        if [ -d /run/systemd/system ]; then
            systemctl restart otelopscol.service
        fi
    fi
fi
