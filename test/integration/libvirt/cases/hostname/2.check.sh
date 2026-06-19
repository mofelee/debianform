assert_remote \
  "hostname was restored to the blank-VM value" \
  "test \"\$(hostnamectl --static)\" = debianform-ci"
