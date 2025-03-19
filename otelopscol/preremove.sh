if [ "$1" != "1" ]; then
    if command -v systemctl >/dev/null 2>&1; then
        systemctl stop otelopscol.service
        systemctl disable otelopscol.service
    fi
fi
