run_remote \
  "change the managed group GID" \
  "groupmod -g 4243 debianform-deploy"
run_remote \
  "change the managed user shell" \
  "usermod -s /bin/sh debianform-app"
run_remote \
  "remove the managed authorized key" \
  ": > /home/debianform-app/.ssh/authorized_keys"
