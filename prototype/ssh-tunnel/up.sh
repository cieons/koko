#!/bin/sh
# PROTOTYPE, throwaway: set up a scratch koko server, a disposable docker Host
# and an OpenSSH client config, all under $1. Nothing outside $1 is written;
# the user's ~/.ssh is never read for config (ssh -F) and never written.
set -eu
S=$1
HERE=$(cd "$(dirname "$0")" && pwd)
export KOKO_PROTO_HOME=$S/home
mkdir -p "$S/bin" "$S/home/server" "$S/home/device" "$S/home/ssh" "$S/keys" "$S/logs"
(cd "$HERE" && go build -o "$S/bin/kokoproto" .)
B=$S/bin/kokoproto

"$B" server -init
[ -f "$S/keys/direct_ed25519" ] || ssh-keygen -q -t ed25519 -N '' -C direct-baseline -f "$S/keys/direct_ed25519"

docker rm -f koko-proto-web1 >/dev/null 2>&1 || true
docker run -d --rm --init --name koko-proto-web1 -p 127.0.0.1:47022:22 \
  -e AUTHORIZED_KEYS="$(cat "$S/home/server/koko_key.pub" "$S/keys/direct_ed25519.pub")" \
  koko-proto-host:latest >/dev/null
for _ in $(seq 50); do docker exec koko-proto-web1 test -f /run/sshd.pid 2>/dev/null && break; sleep 0.1; done
HOSTKEY=$(docker exec koko-proto-web1 cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)
echo "web1 127.0.0.1:47022 deploy $HOSTKEY" > "$S/home/server/hosts"

"$B" join -device alice/laptop -server 127.0.0.1:47443
grep -v '^host-web1 ' "$S/home/ssh/known_hosts" > "$S/home/ssh/known_hosts.new" || true
echo "host-web1 $HOSTKEY" >> "$S/home/ssh/known_hosts.new"
mv "$S/home/ssh/known_hosts.new" "$S/home/ssh/known_hosts"

cat > "$S/home/ssh/config" <<CFG
# The block from #6, as koko would write it to ~/.ssh/koko/ssh_config.
Host *.koko
    ProxyCommand "$B" tunnel '%h'
    HostKeyAlias koko-proto
    UserKnownHostsFile $S/home/ssh/known_hosts
    StrictHostKeyChecking yes
    UpdateHostKeys no
    PreferredAuthentications none
    ForwardAgent no

# The same Tunnel opened by OpenSSH's own -W over a shared Device Connection
# (ControlMaster on the outer connection), to compare against one Device
# Connection per ssh invocation.
Host *.kokomux
    ProxyCommand ssh -F $S/home/ssh/config -W '%h:%p' koko-device
    HostKeyAlias koko-proto
    UserKnownHostsFile $S/home/ssh/known_hosts
    StrictHostKeyChecking yes
    UpdateHostKeys no
    PreferredAuthentications none
    ForwardAgent no
Host *.kokomuxwan
    ProxyCommand ssh -F $S/home/ssh/config -W '%h:%p' koko-device-wan
    HostKeyAlias koko-proto
    UserKnownHostsFile $S/home/ssh/known_hosts
    StrictHostKeyChecking yes
    UpdateHostKeys no
    PreferredAuthentications none
    ForwardAgent no
Host koko-device-wan
    Port 47444
Host koko-device koko-device-wan
    HostName 127.0.0.1
    Port 47443
    User koko
    IdentityFile $S/home/device/id_ed25519
    IdentitiesOnly yes
    HostKeyAlias koko-proto
    UserKnownHostsFile $S/home/ssh/known_hosts
    StrictHostKeyChecking yes
    ControlMaster auto
    ControlPath cm-%C  # relative to $S: the scratch path is past the 104-byte socket limit
    ControlPersist 600

# Baseline without koko: the device straight to the Host.
Host direct-web1
    HostName 127.0.0.1
    Port 47022
    User deploy
    IdentityFile $S/keys/direct_ed25519
    IdentitiesOnly yes
    HostKeyAlias host-web1
    UserKnownHostsFile $S/home/ssh/known_hosts
    StrictHostKeyChecking yes
Host direct-web1-wan
    HostName 127.0.0.1
    Port 47023
    User deploy
    IdentityFile $S/keys/direct_ed25519
    IdentitiesOnly yes
    HostKeyAlias host-web1
    UserKnownHostsFile $S/home/ssh/known_hosts
    StrictHostKeyChecking yes

# Stand-in for the user's own defaults, keeping ssh off the real ~/.ssh.
Host *
    IdentityFile $S/keys/none
    IdentityAgent none
    UserKnownHostsFile $S/home/ssh/known_hosts
CFG
chmod 600 "$S/home/ssh/config"
echo "ready: KOKO_PROTO_HOME=$S/home ssh -F $S/home/ssh/config web1.koko"
