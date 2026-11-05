#!/bin/sh
# PROTOTYPE, throwaway: the measurements behind README.md. Needs up.sh run on
# $1, `kokoproto server` on :47443 and two `kokoproto delay` relays (40 ms
# RTT): :47444 -> :47443 (device to koko server) and :47023 -> :47022
# (device straight to the Host, the baseline).
set -eu
S=$1
export KOKO_PROTO_HOME=$S/home
unset KOKO_PROTO_TRACE
B=$S/bin/kokoproto
C=$S/home/ssh/config
D=$S/data
cd "$S" # ControlPath and sockets are relative: the scratch path is too long
mkdir -p "$D"
h() { printf '\n===== %s\n' "$*"; }
SPID=$(lsof -t -iTCP:47443 -sTCP:LISTEN)
cpu() { ps -o cputime= -p "$SPID" | tr -d ' '; }

h "setup time per invocation: ssh <name> true (loopback)"
"$B" bench -n 30 -warm 2 -label "direct (no koko)" ssh -F "$C" direct-web1 true
"$B" bench -n 30 -warm 2 -label "koko tunnel" ssh -F "$C" web1.koko true
"$B" bench -n 30 -warm 2 -label "shared Device Connection" ssh -F "$C" web1.kokomux true
"$B" bench -n 30 -warm 2 -label "user ControlMaster" ssh -F "$C" -o ControlMaster=auto -o ControlPath=um-%C -o ControlPersist=60 web1.koko true
ssh -F "$C" -o ControlPath=um-%C -O exit web1.koko 2>/dev/null || true

h "setup time per invocation: 40 ms RTT between device and koko server / Host"
"$B" bench -n 15 -warm 1 -label "direct (no koko) wan" ssh -F "$C" direct-web1-wan true
KOKO_PROTO_SERVER=127.0.0.1:47444 "$B" bench -n 15 -warm 1 -label "koko tunnel wan" ssh -F "$C" web1.koko true
"$B" bench -n 15 -warm 1 -label "shared Device Connection wan" ssh -F "$C" web1.kokomuxwan true

h "where the setup time goes (one koko tunnel run, loopback then wan)"
for srv in 127.0.0.1:47443 127.0.0.1:47444; do
  : > "$S/logs/m.trace"
  KOKO_PROTO_SERVER=$srv KOKO_PROTO_TRACE=$S/logs/m.trace ssh -F "$C" web1.koko true
  sed 's/^/  /' "$S/logs/m.trace"
  grep 'ready' "$S/logs/server.log" | tail -1 | sed 's/^/  server: /'
done

h "interactive latency: one keystroke out, its echo back (ssh -tt, raw tty, cat)"
"$B" bench echo -n 300 -label "direct (no koko)" ssh -tt -e none -F "$C" direct-web1 'stty raw -echo; cat'
"$B" bench echo -n 300 -label "koko tunnel" ssh -tt -e none -F "$C" web1.koko 'stty raw -echo; cat'
"$B" bench echo -n 150 -label "direct (no koko) wan" ssh -tt -e none -F "$C" direct-web1-wan 'stty raw -echo; cat'
KOKO_PROTO_SERVER=127.0.0.1:47444 "$B" bench echo -n 150 -label "koko tunnel wan" ssh -tt -e none -F "$C" web1.koko 'stty raw -echo; cat'

h "throughput, 512 MiB, loopback (n=3 each)"
[ -f "$D/512m" ] || head -c 536870912 /dev/urandom > "$D/512m"
N=536870912
ssh -F "$C" direct-web1 "head -c $N /dev/urandom > /tmp/src512m"
for name in direct-web1 web1.koko; do
  c0=$(cpu)
  "$B" bench -n 3 -warm 0 -bytes $N -label "$name raw up" sh -c "ssh -F $C $name 'cat > /dev/null' < $D/512m"
  "$B" bench -n 3 -warm 0 -bytes $N -label "$name raw down" sh -c "ssh -F $C $name 'cat /tmp/src512m' > /dev/null"
  "$B" bench -n 3 -warm 0 -bytes $N -label "$name scp up" scp -q -F "$C" "$D/512m" "$name:/tmp/512m"
  "$B" bench -n 3 -warm 0 -bytes $N -label "$name scp down" scp -q -F "$C" "$name:/tmp/512m" "$D/512m.down"
  "$B" bench -n 3 -warm 0 -bytes $N -label "$name rsync up" sh -c "ssh -F $C $name 'rm -f /tmp/r512m' && rsync -a -e 'ssh -F $C' $D/512m $name:/tmp/r512m"
  echo "  koko server CPU during $name runs: $c0 -> $(cpu)"
done
ssh -F "$C" web1.koko 'head -c 0 /dev/null' # noop
: > "$S/logs/cpu.trace"
KOKO_PROTO_TRACE=$S/logs/cpu.trace ssh -F "$C" web1.koko 'cat > /dev/null' < "$D/512m"
echo "  koko tunnel itself, one 512 MiB upload: $(grep exit "$S/logs/cpu.trace")"

h "throughput, 128 MiB, 40 ms RTT (n=2 each)"
N=134217728
head -c $N "$D/512m" > "$D/128m"
"$B" bench -n 2 -warm 0 -bytes $N -label "direct wan raw up" sh -c "ssh -F $C direct-web1-wan 'cat > /dev/null' < $D/128m"
"$B" bench -n 2 -warm 0 -bytes $N -label "koko wan raw up" sh -c "KOKO_PROTO_SERVER=127.0.0.1:47444 ssh -F $C web1.koko 'cat > /dev/null' < $D/128m"
"$B" bench -n 2 -warm 0 -bytes $N -label "direct wan raw down" sh -c "ssh -F $C direct-web1-wan 'head -c $N /tmp/src512m' > /dev/null"
"$B" bench -n 2 -warm 0 -bytes $N -label "koko wan raw down" sh -c "KOKO_PROTO_SERVER=127.0.0.1:47444 ssh -F $C web1.koko 'head -c $N /tmp/src512m' > /dev/null"
"$B" bench -n 2 -warm 0 -bytes $N -label "shared DC wan raw down" sh -c "ssh -F $C web1.kokomuxwan 'head -c $N /tmp/src512m' > /dev/null"
ssh -F "$C" -O exit koko-device 2>/dev/null || true
ssh -F "$C" -O exit koko-device-wan 2>/dev/null || true
