#!/bin/bash
VPN_SERVER="31.56.78.5"
VPN_USER="20251024172230"
VPN_PASS="GPCEWCEKKD"
VPN_PSK="perwiramedia"
LAC_NAME="webtrvpn"

echo "Setting up IPSec (libreswan) configuration..."
cat > /etc/ipsec.conf << EOF
config setup
    protostack=netkey
    dumpdir=/var/run/pluto/
    nat_traversal=yes
    virtual_private=%v4:10.0.0.0/8,%v4:192.168.0.0/16,%v4:172.16.0.0/12
    oe=off
    listen=0.0.0.0

conn L2TP-PSK
    authby=secret
    pfs=no
    auto=add
    keyingtries=3
    rekey=no
    ikelifetime=8h
    keylife=1h
    type=transport
    left=%defaultroute
    leftprotoport=17/1701
    right=${VPN_SERVER}
    rightprotoport=17/1701
EOF

cat > /etc/ipsec.secrets << EOF
%any ${VPN_SERVER} : PSK "${VPN_PSK}"
EOF

echo "Setting up xl2tpd configuration..."
cat > /etc/xl2tpd/xl2tpd.conf << EOF
[global]
port = 1701

[lac ${LAC_NAME}]
lns = ${VPN_SERVER}
ppp debug = yes
pppoptfile = /etc/ppp/options.l2tpd.${LAC_NAME}
length bit = yes
autodial = yes
redial = yes
redial timeout = 15
EOF

cat > /etc/ppp/options.l2tpd.${LAC_NAME} << EOF
ipcp-accept-local
ipcp-accept-remote
refuse-eap
noccp
noauth
idle 1800
mtu 1410
mru 1410
defaultroute
usepeerdns
debug
connect-delay 5000
name "${VPN_USER}"
password "${VPN_PASS}"
EOF

echo "${VPN_USER} * ${VPN_PASS} *" > /etc/ppp/chap-secrets

echo "Restarting services..."
systemctl restart ipsec
systemctl restart xl2tpd

echo "Checking ipsec status..."
ipsec status

echo "IPSec Up connection..."
ipsec auto --up L2TP-PSK

echo "Triggering L2TP Dial..."
echo "c ${LAC_NAME}" > /var/run/xl2tpd/l2tp-control

sleep 5
echo "Checking PPP interface..."
ip addr show ppp0 2>/dev/null && echo "VPN_CONNECTED_SUCCESS" || echo "VPN_STILL_NOT_CONNECTED - check journalctl -u xl2tpd or systemctl status ipsec"
