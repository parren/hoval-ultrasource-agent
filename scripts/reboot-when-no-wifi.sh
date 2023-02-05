#! /bin/bash
# https://weworkweplay.com/play/rebooting-the-raspberry-pi-when-it-loses-wireless-connection-wifi/
#
# To install:
# $ crontab -e
# then add:
# */1 * * * * /home/peo/ultrasource/reboot-when-no-wifi.sh >> /dev/null 2>&1
#

host=$(ip route get 1.1.1.1 | grep -o '192.168.[.0-9]*' | head -n1)
if [ "$host" == "" ]; then
  host=192.168.178.1
fi

ping -c4 $host > /dev/null
if [ $? != 0 ]; then
  date | sudo tee -a /var/log/reboot-when-no-wifi.log >/dev/null
  echo "Failed to ping $host - rebooting" | sudo tee /dev/kmsg >/dev/null
  sudo shutdown --reboot now
fi
date > /tmp/reboot-when-no-wifi.sh.last
