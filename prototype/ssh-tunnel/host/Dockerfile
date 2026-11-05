# PROTOTYPE, throwaway: a disposable Host for the SSH Tunnel prototype.
FROM alpine:3.22
RUN apk add --no-cache openssh-server openssh-sftp-server openssh-client-common rsync busybox-extras \
 && adduser -D -s /bin/sh deploy \
 && echo 'deploy:*' | chpasswd -e \
 && ssh-keygen -A \
 && mkdir -p /srv/www && echo 'hello from a Service on web1' > /srv/www/index.html
COPY sshd_config /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
