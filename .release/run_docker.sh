#!/bin/bash

if [[ "$OSTYPE" != "darwin"* && "$EUID" -ne 0 ]]; then
  echo "Please run as root or with sudo"
  exit
fi

# Prepare db directory
mkdir -p alphanet
if [[ "$OSTYPE" != "darwin"* ]]; then
  chown -R 65532:65532 alphanet
fi

docker-compose up