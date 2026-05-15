variable "image" {
  type    = string
  default = "ghcr.io/kroexov/apprentice:latest"
}

variable "db_user" {
  type    = string
  default = "postgres"
}

variable "db_name" {
  type    = string
  default = "apprentice"
}

variable "db_password" {
  type    = string
  default = ""
}

variable "sentry_dsn" {
  type    = string
  default = ""
}

job "apisrv" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel     = 1
    min_healthy_time = "10s"
    healthy_deadline = "3m"
    auto_revert      = true
  }

  group "api" {
    count = 1

    restart {
      attempts = 3
      interval = "5m"
      delay    = "25s"
      mode     = "delay"
    }

    # host networking — контейнер видит localhost сервера, включая PostgreSQL на :5432
    network {
      mode = "host"
    }

    task "apisrv" {
      driver = "docker"

      config {
        image        = var.image
        network_mode = "host"
        args         = ["-config", "/local/config.toml", "-json"]
      }

      env {
        DB_USER     = var.db_user
        DB_NAME     = var.db_name
        DB_PASSWORD = var.db_password
        SENTRY_DSN  = var.sentry_dsn
      }

      template {
        data = <<-EOT
[Server]
Host      = "127.0.0.1"
Port      = 8091
IsDevel   = false
EnableVFS = false

[Database]
Addr            = "localhost:5432"
User            = "{{ env "DB_USER" }}"
Database        = "{{ env "DB_NAME" }}"
Password        = "{{ env "DB_PASSWORD" }}"
PoolSize        = 10
ApplicationName = "apisrv"

[Sentry]
DSN         = "{{ env "SENTRY_DSN" }}"
Environment = "production"
EOT
        destination = "local/config.toml"
        change_mode = "restart"
      }

      resources {
        cpu    = 200
        memory = 128
      }

      service {
        name     = "apisrv"
        provider = "nomad"

        check {
          type     = "http"
          path     = "/status"
          port     = 8091
          interval = "10s"
          timeout  = "3s"
        }
      }

      kill_signal  = "SIGTERM"
      kill_timeout = "10s"
    }
  }
}
