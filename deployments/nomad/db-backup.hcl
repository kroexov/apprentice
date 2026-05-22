variable "exclude_dbs" {
  type    = string
  default = "template0,template1,postgres"
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

          EXCLUDE="${EXCLUDE_DBS}"
          TIMESTAMP=$(date +%Y%m%d-%H%M%S)

          echo "[db-backup] fetching database list..."
          DBS=$(sudo -u postgres psql -t -A -c "SELECT datname FROM pg_database WHERE datistemplate = false")

          for DB in $DBS; do
            if echo ",$EXCLUDE," | grep -q ",$DB,"; then
              echo "[db-backup] skipping '$DB' (excluded)"
              continue
            fi

            BACKUP_FILE="/tmp/$DB-$TIMESTAMP.sql.gz"
            echo "[db-backup] dumping '$DB'..."
            sudo -u postgres pg_dump "$DB" | gzip > "$BACKUP_FILE"
            echo "[db-backup] uploading '$DB' to Google Drive..."
            /usr/bin/rclone --config /etc/rclone/rclone.conf copy "$BACKUP_FILE" gdrive:$DB/
            echo "[db-backup] '$DB' uploaded"
            rm -f "$BACKUP_FILE"
            echo "[db-backup] rotating old backups for '$DB' (keeping 5)..."
            /usr/bin/rclone --config /etc/rclone/rclone.conf ls gdrive:$DB/ | sort | head -n -5 | awk '{print $2}' | while read f; do
              /usr/bin/rclone --config /etc/rclone/rclone.conf deletefile gdrive:$DB/$f
              echo "[db-backup] deleted old backup: $f"
            done
          done

          echo "[db-backup] all databases backed up"
        EOF
        ]
      }

      env {
        EXCLUDE_DBS = var.exclude_dbs
      }

      resources {
        cpu    = 200
        memory = 128
      }
    }
  }
}
