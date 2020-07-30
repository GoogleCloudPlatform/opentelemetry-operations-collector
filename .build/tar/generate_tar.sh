#!/bin/bash

CONFIG_FILE=${CONFIG_FILE:-config-mysql_apache_jvm.yaml}
EXPORTERS_DIRECTORY=prometheus_exporters
EXPORTERS_CONFIG_DIRECTORY=exporter_configs

echo "Start building tarball distribution file"

# check config file
if [ ! -e "config/$CONFIG_FILE" ]
then 
    echo "Missing required config file: $CONFIG_FILE"
    exit 1
fi

# move the needed files into dist folder
echo "Organizing files to be compressed"
cp config/$CONFIG_FILE dist/
cp bin/$OTELCOL_BINARY dist/
cp -r config/$EXPORTERS_CONFIG_DIRECTORY dist/

# compress the binary and the config into a .tar file
echo "Compressing..."
cd dist && tar -cvzf google-cloudops-opentelemetry-collector.tar.gz $OTELCOL_BINARY $CONFIG_FILE $EXPORTERS_DIRECTORY $EXPORTERS_CONFIG_DIRECTORY

# remove the folders and files that were added
echo "Cleaning up"
rm $OTELCOL_BINARY $CONFIG_FILE
rm -rf $EXPORTERS_DIRECTORY
rm -rf $EXPORTERS_CONFIG_DIRECTORY
