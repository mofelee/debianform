run_remote "write drift to docker daemon json" \
  "printf '{\"manual-drift\":true}\n' > /etc/docker/daemon.json"
