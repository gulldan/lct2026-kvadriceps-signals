#!/bin/sh
set -eu

# Правила ставятся в сетевом пространстве контейнера до первой страницы. Нужны DNS на
# loopback-резолвере Docker и loopback браузера/CDP; установленные соединения пропускают
# ответы на входящие вызовы сборщика.
iptables-nft-restore <<'RULES'
*filter
:INPUT ACCEPT [0:0]
:FORWARD DROP [0:0]
:OUTPUT DROP [0:0]
-A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
-A OUTPUT -o lo -j ACCEPT
-A OUTPUT -d 0.0.0.0/8 -j REJECT
-A OUTPUT -d 10.0.0.0/8 -j REJECT
-A OUTPUT -d 100.64.0.0/10 -j REJECT
-A OUTPUT -d 169.254.0.0/16 -j REJECT
-A OUTPUT -d 172.16.0.0/12 -j REJECT
-A OUTPUT -d 192.0.0.0/24 -j REJECT
-A OUTPUT -d 192.168.0.0/16 -j REJECT
-A OUTPUT -d 198.18.0.0/15 -j REJECT
-A OUTPUT -d 224.0.0.0/3 -j REJECT
-A OUTPUT -p tcp -m multiport --dports 80,443 -j ACCEPT
COMMIT
RULES
ip6tables-nft-restore <<'RULES'
*filter
:INPUT ACCEPT [0:0]
:FORWARD DROP [0:0]
:OUTPUT DROP [0:0]
-A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
-A OUTPUT -o lo -j ACCEPT
-A OUTPUT -d fc00::/7 -j REJECT
-A OUTPUT -d fe80::/10 -j REJECT
-A OUTPUT -d ff00::/8 -j REJECT
-A OUTPUT -p tcp -m multiport --dports 80,443 -j ACCEPT
COMMIT
RULES

# Код страниц и сервер браузера не могут поменять правила или вернуть root.
exec setpriv --reuid=blessuser --regid=blessuser --init-groups \
    --bounding-set=-net_admin,-net_raw --no-new-privs env -u HOME ./scripts/start.sh
