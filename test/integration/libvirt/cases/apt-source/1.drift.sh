run_remote "save the original target apt source before DebianForm manages it" \
  "cp -a '$DBF_INTEGRATION_TARGET_APT_SOURCE_PATH' /tmp/debianform-original-target.sources && cp -a '$DBF_INTEGRATION_TARGET_APT_SOURCE_PATH' /etc/apt/sources.list.d/debianform-integration.sources"
