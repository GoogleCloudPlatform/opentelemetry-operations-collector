#!/bin/bash

# locate config file
CONFIG_FILE="gcp-config.yaml"
if [ ! -e "$CONFIG_FILE" ]
then 
    echo "missing required config file: $CONFIG_FILE"
    exit 1
fi

# get the repo into local
git clone https://github.com/JingboWangGoogle/opentelemetry-collector-contrib

# build the binary collector
cd opentelemetry-collector-contrib && make otelcontribcol && cd bin

# locate the binary
OTELCOL_BINARY=""
for file in *
do
    OTELCOL_BINARY=$file
done

# move th3 binary back to the root directory
cd ../.. && mv opentelemetry-collector-contrib/bin/$OTELCOL_BINARY .

# compress the binary and the config into a .tar file
tar -cvzf gcp-otel.tar.gz $OTELCOL_BINARY $CONFIG_FILE

# remove the folders and files that were added
rm -rf opentelemetry-collector-contrib
rm $OTELCOL_BINARY