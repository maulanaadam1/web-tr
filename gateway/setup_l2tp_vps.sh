#!/bin/bash
# L2TP VPN Client Setup Script for VPS
# Provider: perwiramedia.com (31.56.78.5)

VPN_SERVER="31.56.78.5"
VPN_USER="20251024172230"
VPN_PASS="GPCEWCEKKD"
LAC_NAME="webtrvpn"

echo "[global]" > /etc/xl2tpd/xl2tpd.conf
echo "port = 1701" >> /etc/xl2tpd/xl2tpd.conf
echo "" >> /etc/xl2tpd/xl2tpd.conf
echo "[lac ${LAC_NAME}]" >> /etc/xl2tpd/xl2tpd.conf
echo "lns = ${VPN_SERVER}" >> /etc/xl2tpd/xl2tpd.conf
echo "ppp debug = yes" >> /etc/xl2tpd/xl2tpd.conf
echo "pppoptfile = /etc/ppp/options.l2tpd.${LAC_NAME}" >> /etc/xl2tpd/xl2tpd.conf
echo "length bit = yes" >> /etc/xl2tpd/xl2tpd.conf
echo "autodial = yes" >> /etc/xl2tpd/xl2tpd.conf
echo "redial = yes" >> /etc/xl2tpd/xl2tpd.conf
echo "redial timeout = 15" >> /etc/xl2tpd/xl2tpd.conf

echo "ipcp-accept-local" > /etc/ppp/options.l2tpd.${LAC_NAME}
echo "ipcp-accept-remote" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "refuse-eap" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "noccp" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "noauth" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "idle 1800" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "mtu 1410" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "mru 1410" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "defaultroute" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "usepeerdns" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "connect-delay 5000" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "name ${VPN_USER}" >> /etc/ppp/options.l2tpd.${LAC_NAME}
echo "password ${VPN_PASS}" >> /etc/ppp/options.l2tpd.${LAC_NAME}

# Also write chap-secrets for PPP authentication
echo "${VPN_USER} * ${VPN_PASS} *" >> /etc/ppp/chap-secrets

# Restart xl2tpd
systemctl restart xl2tpd
systemctl status xl2tpd --no-pager | head -10

echo "CONFIG_DONE"
echo "Attempting to connect to VPN in 2 seconds..."
sleep 2

# Trigger dial
echo "c ${LAC_NAME}" > /var/run/xl2tpd/l2tp-control
sleep 5

# Check if PPP interface appeared
ip addr show ppp0 2>/dev/null && echo "VPN_CONNECTED" || echo "VPN_NOT_CONNECTED_YET - check logs: journalctl -u xl2tpd"
