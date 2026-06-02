job "db-recover" {
  datacenters = ["dc1"]
  type        = "batch"

  parameterized {
    payload       = "forbidden"
    meta_required = ["db_name"]
    meta_optional = ["backup_file"]
  }

  group "recover" {
    task "recover" {
      driver = "raw_exec"

      config {
        command = "/bin/bash"
        args = ["-c", <<-EOF
          set -e

          DB="${NOMAD_META_db_name}"
          RCLONE_CONFIG="/etc/rclone/rclone.conf"

          if [[ -n "${NOMAD_META_backup_file:-}" ]]; then
            CHOSEN="${NOMAD_META_backup_file}"
          else
            echo "[recover] Finding latest backup for '$DB'..."
            CHOSEN=$(rclone --config "$RCLONE_CONFIG" ls "gdrive:$DB/" | sort | tail -1 | awk '{print $2}')
          fi

          if [[ -z "$CHOSEN" ]]; then
            echo "[recover] No backups found for '$DB'"
            exit 1
          fi

          echo "[recover] Downloading '$CHOSEN'..."
          rclone --config "$RCLONE_CONFIG" copy "gdrive:$DB/$CHOSEN" /tmp/

          echo "[recover] Dropping and recreating '$DB'..."
          sudo -u postgres dropdb --if-exists "$DB"
          sudo -u postgres createdb "$DB"

          echo "[recover] Restoring..."
          gunzip -c "/tmp/$CHOSEN" | sudo -u postgres psql "$DB"
          rm -f "/tmp/$CHOSEN"

          echo "[recover] Done. '$DB' restored from '$CHOSEN'."
        EOF
        ]
      }

      resources {
        cpu    = 200
        memory = 128
      }
    }
  }
}
