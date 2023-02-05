#! /bin/bash

# Runs the agent in a tmux session. To attach:
# $ tmux attach-session -t raspi-agent
# To detach again: Ctrl+b d 
#
# To install:
# $ crontab -e
# then add:
# @reboot sleep 20 && /home/peo/ultrasource/auto-run-on-boot.sh

source ~/.bashrc

scriptdir=$(dirname $(realpath "$0"))

echo "[autorun] Enabling CAN bus..."
$scriptdir/enable-can0.sh

echo "[autorun] Starting agent in tmux..."
# -s: session name
# -d: detach
# -A: attach if exists
# -c: current dir
tmux new-session -dA -s raspi-agent -c $scriptdir $scriptdir/run.sh

echo "[autorun] Started."
