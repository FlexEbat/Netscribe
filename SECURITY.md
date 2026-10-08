# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

If the **Security** tab of this repository offers **Report a vulnerability**, use it. Otherwise
contact the maintainer, [@FlexEbat](https://github.com/FlexEbat), directly on GitHub and share the
details only once you have a private channel. Include the version or commit, what you did, what
happened, and what you expected. A proof of concept helps but is not required.

You will get an answer within 7 days. A confirmed problem is fixed in a new release, and
the report is credited if you want that.

## Supported versions

Only the latest release receives security fixes.

## What Netscribe does and does not protect

Netscribe reads your network and stores what it finds, so the database and the config file
are sensitive. The points that matter when you deploy it:

- The config file holds SNMP communities and SSH key paths. Keep it at mode `0600`; the
  service refuses to start with wider permissions unless `--allow-loose-permissions` is set.
- SNMP v2c sends the community in clear text. Use a read-only community and restrict it by ACL
  on the device.
- The web panel serves plain HTTP unless `tls` is configured. Outside loopback, put it behind a
  TLS-terminating reverse proxy or configure `tls`, and list the proxy in `trustedProxies`.
- Scan targets are limited to RFC 1918 ranges, not wider than /16, unless `allowPublicTargets`
  is enabled. Do not enable it for networks you do not own.
