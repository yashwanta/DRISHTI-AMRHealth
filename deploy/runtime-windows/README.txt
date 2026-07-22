DRISHTI AMR Health - Windows Runtime Installer

1. Extract the complete installer ZIP.
2. Open PowerShell as Administrator in the extracted folder.
3. Run:
   powershell -ExecutionPolicy Bypass -File .\Install-DRISHTI-AMRHealth.ps1
4. Enter the Agent API key when prompted.

The bundle contains prebuilt application and PostgreSQL images. It does not
contain the Git repository, Go source, React source, Node.js, or Go toolchain.
The ZIP installer installs Podman through winget when it is missing. The
offline Setup EXE already contains the official Podman installer.

The installer enables WSL 2 when needed. If Windows requires a restart,
installation resumes automatically after the installing user signs in again.

The API key is created as a Podman secret at installation time and is not part
of the installer archive or container image. A local computer administrator can
still inspect local runtime resources. For complete key isolation, configure the
application to call an HTTPS proxy service that owns the upstream LLM key.
