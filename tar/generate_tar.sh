#!/bin/bash
echo "Start building tarball distribution file"
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
echo "Organizing files to be compressed"
cp config/$CONFIG_FILE dist/
cp bin/$OTELCOL_BINARY dist/

# compress the binary and the config into a .tar file
echo "Compressing..."
cd dist && tar -cvzf gcp-otel.tar.gz $OTELCOL_BINARY $CONFIG_FILE

# remove the folders and files that were added
echo "Cleaning up"
rm $OTELCOL_BINARY $CONFIG_FILE