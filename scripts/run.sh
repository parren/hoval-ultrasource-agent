#!/bin/bash

scriptdir=$(dirname $(realpath "$0"))

mkdir -p $scriptdir/logs/

touch /tmp/watchdog-was-here
touch /tmp/agent-was-here

$scriptdir/raspi-agent \
  --google-sheet-id=$(cat $scriptdir/google-sheet-id.txt) \
  --max-sheet-value-age=10m \
  --temperature-sensor=28-3c01f0961954:temp5m \
  --temperature-sensor=28-3c710457683d:temp1m \
  --apply-desired-settings=true \
  --apply-automatic-settings=true \
  --settings-query-interval=2m \
  --log-to-files-dir=$scriptdir/logs/ \
  --log-to-files=true \
  --log-to-files-interval=2m \
  --log-to-sheet=true \
  --log-to-sheet-interval=1h \
  --heartbeat-file=/tmp/agent-was-here \
  --heartbeat-delay=1m
