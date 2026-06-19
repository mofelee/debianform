assert_remote \
  "hostname remains at the blank-VM value after resource removal" \
  "test \"\$(hostnamectl --static)\" = debianform-ci"
