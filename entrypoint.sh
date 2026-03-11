#!/bin/sh
# Ensure the data directory exists and is writable by the atlas user
mkdir -p /home/atlas/.atlas
chown -R atlas:atlas /home/atlas/.atlas

# Drop privileges and run as atlas
export HOME=/home/atlas
exec su-exec atlas /app/atlas
