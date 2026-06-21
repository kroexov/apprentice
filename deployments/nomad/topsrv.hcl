variable "topsrv_pg_password" {
  type    = string
  default = ""
}

variable "vm_url" {
  type    = string
  default = "https://kroexov.webhop.me/metrics"
}

job "topsrv" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel     = 1
    min_healthy_time = "10s"
    healthy_deadline = "3m"
    auto_revert      = true
  }

  ui {
    description = "topsrv metrics — VictoriaMetrics vmui"
    link {
      label = "Dashboard (vmui)"
      url   = "${var.vm_url}/vmui"
    }
    link {
      label = "Raw metrics"
      url   = "http://localhost:9100/metrics"
    }
  }

  group "topsrv" {
    count = 1

    restart {
      attempts = 3
      interval = "5m"
      delay    = "15s"
      mode     = "delay"
    }

    network {
      port "http" {
        static = 9100
      }
    }

    task "topsrv" {
      driver = "raw_exec"

      config {
        command = "/usr/local/bin/topsrv"
        args    = ["-config", "${NOMAD_TASK_DIR}/topsrv.toml"]
      }

      env {
        TOPSRV_PG_PASSWORD = var.topsrv_pg_password
      }

      template {
        data = <<-EOT
[Server]
Listen = ":9100"

[Push]
Endpoint = "http://localhost:8428/api/v1/import/prometheus"
Interval  = "15s"
SpoolDir  = "/tmp/topsrv-spool"

[Postgres]
DSN = "postgres://topsrv:{{ env "TOPSRV_PG_PASSWORD" }}@localhost:5432/postgres?sslmode=disable"

[Nginx]
StubStatusURL = "http://127.0.0.1:8080/stub_status"
AccessLogs    = ["/var/log/nginx/access.log"]
EOT
        destination = "local/topsrv.toml"
        change_mode = "restart"
      }

      resources {
        cpu    = 100
        memory = 64
      }

      service {
        name     = "topsrv"
        port     = "http"
        provider = "nomad"

        check {
          type     = "http"
          path     = "/status"
          interval = "15s"
          timeout  = "3s"
        }
      }

      kill_signal  = "SIGTERM"
      kill_timeout = "10s"
    }
  }
}
