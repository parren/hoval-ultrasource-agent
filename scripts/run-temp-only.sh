#!/bin/bash
./raspi-agent --enable-can-bus=false --settings-query-interval=10s \
  --google-sheet-id=$(cat google-sheet-id.txt) \
  --temperature-sensor=28-3c01f0961954:temp5m \
  --temperature-sensor=28-3c710457683d:temp1m
