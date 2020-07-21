#!/bin/bash

# locate config file
CONFIG_FILE="gcp-config.yaml"
OTELCOL_BINARY="otelopscol"

if [ ! -e "config/$CONFIG_FILE" ]
then 
    echo "missing required config file: $CONFIG_FILE"
    exit 1
fi

GO111MODULE=on CGO_ENABLE=0 go build -o ./tar/$OTELCOL_BINARY ./cmd/otelopscol

# move the binary back to the root directory
cp config/$CONFIG_FILE tar/

# compress the binary and the config into a .tar file
cd tar && tar -cvzf gcp-otel.tar.gz $OTELCOL_BINARY $CONFIG_FILE

# remove the folders and files that were added
rm $OTELCOL_BINARY $CONFIG_FILE