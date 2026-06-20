profile "bad" {
  system {
    hostname = "should-not-be-in-profile"
  }
}

host "invalid1" {
  imports = [profile.bad]
}
