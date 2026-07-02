# Project Structure

DRISHTI AMR Health keeps runtime data local while organizing source, deployment, and helper tools by purpose.

```text
.github/workflows/        GitHub Actions validation workflows
backend/                  Go backend and local RDS proxy
frontend/                 React + TypeScript UI
data/config/              Sanitized config example only; real config is ignored
deploy/install/           Windows and Linux installer implementations
deploy/compose/           Optional Podman Compose file
docs/                     Operator and API discovery docs
scripts/                  Release/package automation
tools/rds/                Local RDS snapshot helper scripts
```

Root-level installer launchers remain for convenience:

```text
Install-DRISHTI-Windows.ps1
install-drishti-linux.sh
```

Those wrappers call the real installer implementations in `deploy/install/`.