DRISHTI AMR Health - Linux Plant Server Runtime Installer

Supported server families:
- Ubuntu / Debian
- RHEL / Rocky / Alma / Fedora
- openSUSE / SLES

Install:
  tar -xzf DRISHTI-AMRHealth-Runtime-Linux-1.0.0.tar.gz
  cd DRISHTI-AMRHealth-Runtime-Linux-1.0.0
  sudo bash ./install-drishti-amr-health.sh --open-firewall

For a fixed plant-server address:
  sudo bash ./install-drishti-amr-health.sh \
    --public-url http://10.222.42.10:8099 \
    --open-firewall

The installer loads prebuilt application and PostgreSQL images, creates a
systemd service, binds TCP 8099 on all server interfaces, and prints the URL
for plant users. Git, Go, Node.js, npm, and application source are not shipped.

The Agent API key is entered during installation and stored as a Podman secret;
it is not part of the installer archive or application image. Root on the plant
server can still inspect local runtime resources. A server-side LLM proxy is
required when the upstream key must never be present at a plant.

Operations:
  sudo systemctl status drishti-amr-health
  sudo systemctl restart drishti-amr-health
  sudo podman logs -f AMR-Health
