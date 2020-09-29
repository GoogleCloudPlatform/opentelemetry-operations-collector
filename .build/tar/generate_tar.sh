#!/bin/bash

# Copyright 2020, Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

CONFIG_FILE=${CONFIG_FILE:-config-example.yaml}
EXPORTERS_DIRECTORY=prometheus_exporters
EXPORTERS_CONFIG_DIRECTORY=exporter_configs
FINAL_CONFIG_FILE=config.yaml
README=${README:-tarball-readme.md}

echo "Start building tarball distribution file..."

# check config file
if [ ! -e "config/$CONFIG_FILE" ]
then 
    echo "Missing required config file: $CONFIG_FILE"
    exit 1
fi

mkdir -p dist

# move the needed files into dist folder
echo "Organizing files to be compressed..."
cp config/$CONFIG_FILE dist/
cp bin/$OTELCOL_BINARY dist/
cp -r config/$EXPORTERS_CONFIG_DIRECTORY dist/
cp docs/$README dist/

mv dist/$CONFIG_FILE dist/$FINAL_CONFIG_FILE

# compress the binary and the config into a .tar file
echo "Compressing..."
cd dist && tar -cvzf google-cloud-metrics-agent.tar.gz $OTELCOL_BINARY $FINAL_CONFIG_FILE $README $EXPORTERS_DIRECTORY $EXPORTERS_CONFIG_DIRECTORY

# remove the folders and files that were added
echo "Clean up..."
rm $OTELCOL_BINARY $FINAL_CONFIG_FILE $README
rm -rf $EXPORTERS_DIRECTORY
rm -rf $EXPORTERS_CONFIG_DIRECTORY
