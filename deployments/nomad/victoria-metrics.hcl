job "victoria-metrics" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel     = 1
    min_healthy_time = "10s"
    healthy_deadline = "3m"
    auto_revert      = true
  }

  group "vm" {
    count = 1

    restart {
      attempts = 3
      interval = "5m"
      delay    = "15s"
      mode     = "delay"
    }

    network {
      port "http" {
        static = 8428
      }
    }

    task "victoria-metrics" {
      driver = "raw_exec"

      config {
        command = "/usr/local/bin/victoria-metrics"
        args = [
          "-httpListenAddr=:8428",
          "-storageDataPath=/var/lib/victoria-metrics",
          "-retentionPeriod=30d",
        ]
      }

      resources {
        cpu    = 200
        memory = 256
      }

      service {
        name     = "victoria-metrics"
        port     = "http"
        provider = "nomad"

        check {
          type     = "http"
          path     = "/health"
          interval = "15s"
          timeout  = "3s"
        }
      }

      kill_signal  = "SIGTERM"
      kill_timeout = "10s"
    }
  }
}
