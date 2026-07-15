if [[ "$EXPECTED_DISTRIBUTION" == "ubuntu" ]]; then
  run_remote "snapshot stock Netplan files and networkd service state" \
    "set -eu; export LC_ALL=C; find /etc/netplan -maxdepth 1 -type f -name '*.yaml' -print0 | sort -z | xargs -0 -r sha256sum > /tmp/debianform-netplan.sha256; test -s /tmp/debianform-netplan.sha256; systemctl is-active systemd-networkd.service > /tmp/debianform-networkd-active 2>&1 || true; systemctl is-enabled systemd-networkd.service > /tmp/debianform-networkd-enabled 2>&1 || true"

  conflict_config="$CASE_DIR/netplan-conflict.dbf.hcl"
  log "validating stock Netplan conflict fixture"
  dbf validate -f "$conflict_config" >"$LOG_DIR/netplan-conflict.validate.log"

  log "verifying stock Netplan ownership rejects structured and raw networkd plan"
  if dbf plan -f "$conflict_config" >"$LOG_DIR/netplan-conflict.plan.log" 2>&1; then
    cat "$LOG_DIR/netplan-conflict.plan.log"
    fail "Netplan-owned Ubuntu unexpectedly accepted native networkd plan"
  fi
  cat "$LOG_DIR/netplan-conflict.plan.log"

  log "verifying stock Netplan ownership rejects apply before mutation"
  if dbf apply -f "$conflict_config" --auto-approve >"$LOG_DIR/netplan-conflict.apply.log" 2>&1; then
    cat "$LOG_DIR/netplan-conflict.apply.log"
    fail "Netplan-owned Ubuntu unexpectedly applied native networkd resources"
  fi
  cat "$LOG_DIR/netplan-conflict.apply.log"

  for evidence in \
    'active Netplan ownership' \
    '/etc/netplan/50-cloud-init.yaml' \
    'host.cihost.systemd.networkd.network["90-dbf-netplan-structured"]' \
    'host.cihost.files.file["/etc/systemd/network/91-dbf-netplan-raw.network"]' \
    'no provider changes were made' \
    'prepare a native-networkd target outside DebianForm'; do
    grep -F "$evidence" "$LOG_DIR/netplan-conflict.plan.log" >/dev/null
    grep -F "$evidence" "$LOG_DIR/netplan-conflict.apply.log" >/dev/null
  done

  assert_remote "Netplan conflict caused no network file, service, or state-resource mutation" \
    "set -eu; test ! -e /etc/systemd/network/90-dbf-netplan-structured.network; test ! -e /etc/systemd/network/91-dbf-netplan-raw.network; test ! -e /var/lib/debianform-integration/netplan-preflight-state.json || grep -F '\"resources\": {}' /var/lib/debianform-integration/netplan-preflight-state.json; systemctl is-active systemd-networkd.service > /tmp/debianform-networkd-active.current 2>&1 || true; systemctl is-enabled systemd-networkd.service > /tmp/debianform-networkd-enabled.current 2>&1 || true; cmp -s /tmp/debianform-networkd-active /tmp/debianform-networkd-active.current; cmp -s /tmp/debianform-networkd-enabled /tmp/debianform-networkd-enabled.current; rm -f /tmp/debianform-networkd-active.current /tmp/debianform-networkd-enabled.current"
  assert_remote "Netplan conflict left stock Netplan files byte-for-byte unchanged" \
    "set -eu; export LC_ALL=C; find /etc/netplan -maxdepth 1 -type f -name '*.yaml' -print0 | sort -z | xargs -0 -r sha256sum > /tmp/debianform-netplan.current; cmp -s /tmp/debianform-netplan.sha256 /tmp/debianform-netplan.current; rm -f /tmp/debianform-netplan.current"
fi
