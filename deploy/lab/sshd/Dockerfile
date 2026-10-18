FROM alpine:3.22
RUN apk add --no-cache openssh-server && ssh-keygen -A
# No account can log in: the lab only needs port 22 and the SSH banner.
CMD ["/usr/sbin/sshd", "-D", "-e", "-o", "PasswordAuthentication=no"]
