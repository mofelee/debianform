run_remote "stop the compose project and write valid compose yaml drift" \
  "docker compose -p app -f /opt/debianform-compose-app/compose.yaml stop && cat > /opt/debianform-compose-app/compose.yaml <<'YAML'
services:
  web:
    image: busybox:1.36
    command: [\"sh\", \"-c\", \"while true; do echo manual-drift; sleep 60; done\"]
    labels:
      com.example.debianform: \"manual-drift\"
YAML"
run_remote "disable generated compose systemd unit" \
  "systemctl disable debianform-compose-app.service"
