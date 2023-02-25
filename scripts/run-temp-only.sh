#!/bin/bash
mkdir -p temp-logs/
./raspi-agent --enable-can-bus=false \
  --temperature-sensor=28-3c01f0961954:temp5m \
  --temperature-sensor=28-3c710457683d:temp1m \
  --settings-query-interval=10s \
  --log-to-files-dir=$scriptdir/logs/ \
  --log-to-files=true \
  --log-to-files-interval=2m
