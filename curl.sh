#!/bin/sh
curl -L -i "$1" --resolve "$1:80:127.0.0.1"
