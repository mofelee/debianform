assert_remote "initial timezone differs from desired timezone" \
  "test \"\$(timedatectl show -p Timezone --value)\" != 'Asia/Shanghai'"
run_remote "seed default locale with an unmanaged LC_TIME value" \
  "printf 'LANG=C\nLC_TIME=C.UTF-8\n' > /etc/default/locale"
assert_remote "initial default locale differs from desired locale" \
  "! grep -Eq '^LANG=\"?en_US.UTF-8\"?$' /etc/default/locale"
