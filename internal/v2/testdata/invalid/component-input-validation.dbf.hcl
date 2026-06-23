component "reverse_proxy" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
    }))

    validation {
      condition = alltrue([
        for listener in input.listeners :
        listener.port >= 1 && listener.port <= 65535
      ])
      error_message = "Each listener.port must be between 1 and 65535."
    }
  }
}

host "edge1" {
  component "proxy" {
    source = component.reverse_proxy

    inputs = {
      listeners = [
        {
          name = "bad"
          port = 70000
        },
      ]
    }
  }
}
