variable "db_name" {
  type    = string
  default = "apprentice"
}

variable "db_user" {
  type    = string
  default = "postgres"
}

job "db-backup" {
  datacenters = ["dc1"]
  type        = "batch"

  periodic {
    crons            = ["0 3 1,15 * *"]
    prohibit_overlap = true
  }

  group "backup" {
    task "backup" {
      driver = "raw_exec"

      config {
        command = "/bin/bash"
        args = ["-c", <<-EOF
          set -e
          BACKUP_FILE="/tmp/apprentice-$(date +%Y%m%d-%H%M%S).sql.gz"
          echo "[db-backup] starting backup of database '${DB_NAME}'"
          pg_dump -U ${DB_USER} ${DB_NAME} | gzip > "$BACKUP_FILE"
          echo "[db-backup] dump created: $BACKUP_FILE"
          echo "[db-backup] uploading to Google Drive (apprentice/)..."
          /usr/bin/rclone --config /etc/rclone/rclone.conf copy "$BACKUP_FILE" gdrive:apprentice/
          echo "[db-backup] upload complete"
          echo "[db-backup] cleaning up local file"
          rm -f "$BACKUP_FILE"
          echo "[db-backup] done"
        EOF
        ]
      }

      env {
        DB_NAME = var.db_name
        DB_USER = var.db_user
        PGHOST  = "localhost"
      }

      resources {
        cpu    = 200
        memory = 128
      }
    }
  }
}
