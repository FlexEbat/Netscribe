FROM alpine:3.22
RUN apk add --no-cache net-snmp
COPY snmpd.conf /etc/snmp/snmpd.conf
# -C ignores the default config files, -f stays in the foreground, -Lo logs to stdout.
CMD ["snmpd", "-f", "-C", "-c", "/etc/snmp/snmpd.conf", "-Lo"]
