#!/bin/sh
# PROTOTYPE, throwaway: the functional checks behind README.md. Needs up.sh
# run on $1 and `kokoproto server` running. Prints what each tool printed.
set -u
S=$1
export KOKO_PROTO_HOME=$S/home KOKO_PROTO_TRACE=$S/logs/tunnel.trace
B=$S/bin/kokoproto
C=$S/home/ssh/config
D=$S/data
mkdir -p "$D"
h() { printf '\n===== %s\n' "$*"; }
run() { printf '$ %s\n' "$*"; "$@"; printf '[exit %s]\n' "$?"; }

h "1. ssh: what the OpenSSH client does with none (ssh -vvv, auth lines)"
ssh -vvv -F "$C" web1.koko true 2>&1 | grep -E 'Executing proxy|Remote protocol|hostkeyalias|is known|SSH2_MSG_SERVICE|send packet: type 5[0-9]|receive packet: type 5[0-9]|Authentications that|Next authentication|Authenticated to|identity file|Will attempt|preferred|Requesting' | sed 's/^/  /'

h "2. ssh: a shell with a pty, an exec, exit status and stderr"
run ssh -F "$C" -tt web1.koko 'tty; echo out; echo err >&2; exit 7'

h "3. scp (sftp protocol), scp -O (legacy), sftp, rsync, with checksums"
head -c 33554432 /dev/urandom > "$D/32m"
SUM=$(shasum -a 256 < "$D/32m" | cut -c1-16)
echo "local 32m sha256 $SUM"
run scp -q -F "$C" "$D/32m" web1.koko:/tmp/up-scp
run scp -q -O -F "$C" "$D/32m" web1.koko:/tmp/up-scp-legacy
run scp -q -F "$C" web1.koko:/tmp/up-scp "$D/down-scp"
printf 'put %s /tmp/up-sftp\nget /tmp/up-sftp %s\n' "$D/32m" "$D/down-sftp" > "$D/sftp.batch"
run sftp -q -F "$C" -b "$D/sftp.batch" web1.koko
run rsync -a -e "ssh -F $C" "$D/32m" web1.koko:/tmp/up-rsync
run rsync -a -e "ssh -F $C" web1.koko:/tmp/up-rsync "$D/down-rsync"
ssh -F "$C" web1.koko 'for f in /tmp/up-scp /tmp/up-scp-legacy /tmp/up-sftp /tmp/up-rsync; do echo "$f $(sha256sum < $f | cut -c1-16)"; done'
for f in down-scp down-sftp down-rsync; do echo "$D/$f $(shasum -a 256 < "$D/$f" | cut -c1-16)"; done
echo "rsync --version: $(rsync --version | head -1); Host: $(ssh -F "$C" web1.koko 'rsync --version | head -1')"

h "4. port forwards: -L to a Service on the Host's loopback, -D, -R"
ssh -F "$C" -N -L 127.0.0.1:47180:127.0.0.1:8080 web1.koko & P=$!
sleep 1; run curl -s http://127.0.0.1:47180/; kill $P; wait $P 2>/dev/null
ssh -F "$C" -N -D 127.0.0.1:47181 web1.koko & P=$!
sleep 1; run curl -s --socks5-hostname 127.0.0.1:47181 http://127.0.0.1:8080/; kill $P; wait $P 2>/dev/null
mkdir -p "$D/www"; echo "hello from the device" > "$D/www/index.html"
(cd "$D/www" && exec python3 -m http.server -b 127.0.0.1 47183 >/dev/null 2>&1) & W=$!
sleep 1
run ssh -F "$C" -R 127.0.0.1:47182:127.0.0.1:47183 web1.koko 'sleep 0.5; wget -qO- http://127.0.0.1:47182/'
kill $W; wait $W 2>/dev/null

h "5. how the Host name travels: case, user, port, HostName"
run ssh -F "$C" -p 2222 bob@WEB1.koko 'echo "Host Account: $(id -un)"'
grep -E 'auth none|tunnel for' "$S/logs/server.log" | tail -2 | sed 's/^/  server: /'
run ssh -G -F "$C" -p 2222 bob@WEB1.koko | grep -E '^(hostname|proxycommand|hostkeyalias) '

h "6. a name koko does not know"
run ssh -F "$C" web9.koko true

h "7. agent and X11 forwarding asked for on the command line"
cd "$S"  # relative socket paths: the scratch path is past the 104-byte socket limit
eval "$(ssh-agent -a agent.sock)" >/dev/null  # a scratch agent, never the user's
run ssh -F "$C" -A web1.koko 'echo "SSH_AUTH_SOCK=${SSH_AUTH_SOCK:-unset}"'
run ssh -F "$C" -X web1.koko 'echo "DISPLAY=${DISPLAY:-unset}"'
ssh-agent -k >/dev/null; rm -f "$S/agent.sock"
run ssh -F "$C" -o SendEnv=LANG web1.koko 'echo "LANG=${LANG:-unset}"'
grep -E 'refused' "$S/logs/server.log" | tail -3 | sed 's/^/  server: /'

h "8. the Host is down (a recorded Host on a closed port)"
grep -q '^web2 ' "$S/home/server/hosts" || echo "web2 127.0.0.1:47099 deploy $(cut -d' ' -f4- "$S/home/server/hosts" | head -1)" >> "$S/home/server/hosts"
run ssh -F "$C" web2.koko true

h "9. koko tunnel alone: stdin EOF at once, and SIGHUP while open"
: > "$KOKO_PROTO_TRACE"
start=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
"$B" tunnel web1.koko < /dev/null > "$D/eof.out"; code=$?
end=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
echo "stdin </dev/null: exit $code after $(echo "$end - $start" | bc)s; stdout: $(head -c 40 "$D/eof.out")"
cat "$KOKO_PROTO_TRACE" | sed 's/^/  /'
: > "$KOKO_PROTO_TRACE"
(sleep 30) | "$B" tunnel web1.koko > /dev/null & T=$!
sleep 0.5; kill -HUP $T; wait $T; echo "SIGHUP with stdin open: exit $?"
cat "$KOKO_PROTO_TRACE" | sed 's/^/  /'
sleep 0.2; grep -E 'inner handshake|closed after' "$S/logs/server.log" | tail -2 | sed 's/^/  server: /'

h "10. Ctrl-C on ssh without a tty (SIGINT to the whole process group)"
for mode in ignore default; do
  : > "$KOKO_PROTO_TRACE"
  KOKO_PROTO_SIGINT=$mode perl -e '$SIG{INT}="DEFAULT"; setpgrp(0,0); exec @ARGV' ssh -F "$C" web1.koko 'sleep 30' & P=$!
  sleep 1.5; kill -INT -$P; wait $P; code=$?
  echo "koko tunnel SIGINT=$mode: ssh exit $code"
  sed 's/^/  /' "$KOKO_PROTO_TRACE"
done

h "11. ControlMaster on the user's ssh: the error channel under ControlPersist"
run ssh -F "$C" -o ControlMaster=auto -o ControlPath=um-%C -o ControlPersist=30 web9.koko true
ssh -F "$C" -o ControlPath=um-%C -O exit web9.koko 2>/dev/null

h "12. the device has not joined (no device key)"
mv "$S/home/device/id_ed25519" "$S/home/device/id_ed25519.aside"
run ssh -F "$C" web1.koko true
mv "$S/home/device/id_ed25519.aside" "$S/home/device/id_ed25519"

h "13. the koko server presents a key other than the pinned one"
cp "$S/home/device/server" "$S/home/device/server.aside"
echo "127.0.0.1:47443 SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" > "$S/home/device/server"
run ssh -F "$C" web1.koko true
mv "$S/home/device/server.aside" "$S/home/device/server"

h "14. the koko server is down"
run env KOKO_PROTO_SERVER=127.0.0.1:47499 ssh -F "$C" web1.koko true

h "15. the Device Session is revoked during an open SSH Session, then used again"
(sleep 2; "$B" revoke -device alice/laptop) &
run ssh -F "$C" -tt web1.koko 'for i in 1 2 3 4 5 6 7 8; do echo tick $i; sleep 0.5; done'
grep -E 'revoked|closed after' "$S/logs/server.log" | tail -3 | sed 's/^/  server: /'
run ssh -F "$C" web1.koko true
grep -E 'auth method=publickey' "$S/logs/server.log" | tail -2 | sed 's/^/  server: /'
"$B" join -device alice/laptop -server 127.0.0.1:47443 >/dev/null
echo "(re-joined as a fresh Device Session)"
