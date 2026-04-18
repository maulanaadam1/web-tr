#!/bin/bash
export DEBIAN_FRONTEND=noninteractive
dpkg --configure -a --force-confdef --force-confold
apt-get install -f -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold"
apt-get install -y libreswan -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold"
echo "LIBRESWAN_INSTALL_DONE"
