#!/bin/bash

# The use of >/dev/null is to supress stdout, but allow stderr to come thorugh
# This is because installing the tools and building makes a lot of output

echo "Installing tools needed"

# Install git and maven for building the exporters
sudo apt-get update >/dev/null
sudo apt-get install -y git >/dev/null
sudo apt-get install -y maven >/dev/null

# check dist folder
if [ ! -d "dist" ]
then
    echo "Not found: dist folder, creating the folder dist"
    mkdir dist
fi

# Check for prometheus_exporters folder
if [ ! -d "dist/prometheus_exporters" ]
then
    echo "Not found: prometheus_exporters folder, creating the folder"
    mkdir dist/prometheus_exporters
fi

echo "Building Prometheus exporters"

# Clone mysqld exporter, build the binary and put it in the folder
echo "Cloning and building the MySQL exporter"

git clone https://github.com/prometheus/mysqld_exporter.git -q
cd mysqld_exporter
make >/dev/null
cd ..
cp mysqld_exporter/mysqld_exporter dist/prometheus_exporters/mysqld_exporter
rm -rf mysqld_exporter

# Clone Apache exporter, build the binary and put it in the folder
echo "Cloning and building the Apache exporter"

git clone https://github.com/Lusitaniae/apache_exporter.git -q
cd apache_exporter
make >/dev/null
cd ..
cp apache_exporter/apache_exporter dist/prometheus_exporters/apache_exporter
rm -rf apache_exporter

# Clone JVM exporter, build the binary and put it in the folder
echo "Cloning and building the JVM exporter"

git clone https://github.com/prometheus/jmx_exporter.git -q
cd jmx_exporter
version=$(sed -n -e 's#.*<version>\(.*-SNAPSHOT\)</version>#\1#p' pom.xml)
mvn package >/dev/null
cd ..
cp jmx_exporter/jmx_prometheus_httpserver/target/jmx_prometheus_httpserver-${version}-jar-with-dependencies.jar dist/prometheus_exporters/jmx_exporter.jar
rm -rf jmx_exporter

# StatsD exporter
echo "Cloning and building the StatsD exporter"

git clone https://github.com/prometheus/statsd_exporter.git -q
cd statsd_exporter/ 
make build
cd ..
cp statsd_exporter/statsd_exporter dist/prometheus_exporters/statsd_exporter
rm -rf statsd_exporter

echo "Prometheus exporters built"
