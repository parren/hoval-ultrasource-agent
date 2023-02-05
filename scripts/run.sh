#!/bin/bash

scriptdir=$(dirname $(realpath "$0"))

$scriptdir/raspi-agent \
  --temperature-sensor=28-3c01f0961954:temp5m \
  --temperature-sensor=28-3c710457683d:temp1m