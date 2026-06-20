assert_remote \
  "managed hostname was applied" \
  "test \"\$(hostnamectl --static)\" = debianform-managed"
