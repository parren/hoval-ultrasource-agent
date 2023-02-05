#! /bin/bash
sudo ip link set down can0 && \
  sudo ip link set can0 type can bitrate 50000 restart-ms 100 listen-only off && \
  sudo ip link set up can0
