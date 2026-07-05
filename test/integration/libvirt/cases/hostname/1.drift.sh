assert_remote "initial static hostname differs from desired hostname" \
  "test \"\$(hostnamectl --static)\" != 'dbf-hostname'"
assert_remote "initial static hostname is the cloud-init default" \
  "test \"\$(hostnamectl --static)\" = 'debianform-ci'"
