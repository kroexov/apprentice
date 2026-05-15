variable "binary" {
  type    = string
  default = "/usr/local/bin/apisrv"
}

variable "config" {
  type    = string
  default = "/etc/apisrv/config.toml"
}

# Local dev job using raw_exec driver.
# Usage: make nomad-run-local
# Requires: nomad agent -dev (in a separate terminal)
job "apisrv-local" {
  datacenters = ["dc1"]
  type        = "service"

  group "api" {
    count = 1

    network {
      port "http" {
        static = 8075
      }
    }

    task "apisrv" {
      driver = "raw_exec"

      config {
        command = var.binary
        args    = ["-config", var.config, "-dev"]
      }

      resources {
        cpu    = 500
        memory = 256
      }

      service {
        name     = "apisrv"
        port     = "http"
        provider = "nomad"

        check {
          type     = "http"
          path     = "/status"
          interval = "10s"
          timeout  = "3s"
        }
      }

      kill_signal  = "SIGTERM"
      kill_timeout = "10s"
    }
  }
}
