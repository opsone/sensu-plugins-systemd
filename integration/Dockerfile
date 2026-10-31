FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv dbus \
 && rm -rf /var/lib/apt/lists/*
COPY broken.service flappy.service /etc/systemd/system/
COPY check-systemd /usr/local/bin/check-systemd
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
