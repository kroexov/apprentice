job "frontend-deploy" {
  datacenters = ["dc1"]
  type        = "batch"

  group "deploy" {
    task "deploy" {
      driver = "raw_exec"

      config {
        command = "/bin/bash"
        args = ["-c", <<-EOF
          set -e
          echo "[frontend-deploy] starting deployment"
          echo "[frontend-deploy] clearing /var/www/apprentice/"
          rm -rf /var/www/apprentice/*
          echo "[frontend-deploy] unpacking archive..."
          unzip -o /tmp/apprentice-frontend.zip -d /var/www/apprentice
          echo "[frontend-deploy] patching RPC endpoint in index.html"
          sed -i "s|// window.RPC_ENDPOINT = 'http://localhost:8075/v1/rpc/';|window.RPC_ENDPOINT = '/apprentice/v1/rpc/';|" /var/www/apprentice/index.html
          echo "[frontend-deploy] setting permissions"
          chown -R www-data:www-data /var/www/apprentice
          echo "[frontend-deploy] done"
        EOF
        ]
      }

      resources {
        cpu    = 100
        memory = 64
      }
    }
  }
}
