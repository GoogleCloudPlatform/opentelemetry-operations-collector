#!/bin/bash

CONFIG_FILE=${CONFIG_FILE:-config-example.yaml}
README=${README:-tarball-readme.md}
RUN=run.sh

echo "Start building tarball distribution file..."

# check config file
if [ ! -e "config/$CONFIG_FILE" ]
then 
    echo "Missing required config file: $CONFIG_FILE"
    exit 1
fi

# check dist folder
if [ ! -d "dist" ]
then
    echo "Not found: dist folder, creating the folder dist"
    mkdir dist
fi

# move the needed files into dist folder
echo "Organizing files to be compressed..."
cp config/$CONFIG_FILE dist/
cp bin/$OTELCOL_BINARY dist/
cp docs/$README dist/
cp .build/tar/$RUN dist/

# compress the binary and the config into a .tar file
echo "Compressing..."
cd dist && tar -cvzf google-cloudops-opentelemetry-collector.tar.gz $OTELCOL_BINARY $CONFIG_FILE $README $RUN

# remove the folders and files that were added
echo "Clean up..."
rm $OTELCOL_BINARY $CONFIG_FILE $README $RUN
