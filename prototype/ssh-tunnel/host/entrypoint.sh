#!/bin/sh
# PROTOTYPE: install the given keys for the Host Account, start a Service, run sshd.
set -e
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
printf '%s\n' "$AUTHORIZED_KEYS" > /home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys
# The Service: an HTTP server on the Host's loopback only, reached with ssh -L / -D.
busybox-extras httpd -p 127.0.0.1:8080 -h /srv/www
exec /usr/sbin/sshd -D -e
