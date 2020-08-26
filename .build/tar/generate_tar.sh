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
FINAL_CONFIG_FILE=config.yaml
README=${README:-tarball-readme.md}

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

mv dist/$CONFIG_FILE dist/$FINAL_CONFIG_FILE

# compress the binary and the config into a .tar file
echo "Compressing..."
cd dist && tar -cvzf google-cloudops-opentelemetry-collector.tar.gz $OTELCOL_BINARY $FINAL_CONFIG_FILE $README

# remove the folders and files that were added
echo "Clean up..."
rm $OTELCOL_BINARY $FINAL_CONFIG_FILE $README
