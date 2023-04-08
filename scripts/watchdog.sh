#!/bin/bash

# Restarts the agent if it failed to report.
#
# To install to run every 5 minutes:
# $ crontab -e
# then add:
# */5 * * * * /home/peo/ultrasource/watchdog.sh >> /dev/null 2>&1

scriptdir=$(dirname $(realpath "$0"))

if [[ /tmp/watchdog-was-here -nt /tmp/agent-was-here ]]; then
  date | sudo tee -a /var/log/watchdog.log >/dev/null
  killall --wait raspi-agent 2>&1 | sudo tee -a /var/log/watchdog.log >/dev/null
  sleep 10s
  $scriptdir/auto-run-on-boot.sh
fi

touch /tmp/watchdog-was-here
