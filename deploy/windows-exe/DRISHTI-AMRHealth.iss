#define AppVersion GetEnv("DRISHTI_INSTALLER_VERSION")
#define PayloadRoot GetEnv("DRISHTI_PAYLOAD_ROOT")
#define PodmanInstaller GetEnv("DRISHTI_PODMAN_INSTALLER")
#define OutputRoot GetEnv("DRISHTI_OUTPUT_ROOT")

[Setup]
AppId={{B5A2E6BB-24C8-4EA8-A3AB-93DCBD48FA05}
AppName=DRISHTI AMR Health
AppVersion={#AppVersion}
AppPublisher=DRISHTI
DefaultDirName={autopf}\DRISHTI AMR Health
DisableDirPage=yes
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputRoot}
OutputBaseFilename=DRISHTI-AMRHealth-Setup-{#AppVersion}-Windows-x64
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
Uninstallable=no
SetupLogging=yes

[Files]
Source: "{#PodmanInstaller}"; DestDir: "{tmp}"; DestName: "podman-installer-windows-amd64.exe"; Flags: deleteafterinstall
Source: "{#PayloadRoot}\*"; DestDir: "{commonappdata}\DRISHTI-AMRHealth\installer"; Flags: recursesubdirs createallsubdirs

[Run]
Filename: "{tmp}\podman-installer-windows-amd64.exe"; Parameters: "/install /quiet /norestart"; StatusMsg: "Installing the bundled Podman runtime..."; Flags: waituntilterminated; Check: not PodmanInstalled
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{commonappdata}\DRISHTI-AMRHealth\installer\Install-DRISHTI-AMRHealth.ps1"" -SkipPodmanInstall"; StatusMsg: "Loading and starting DRISHTI AMR Health..."; Flags: waituntilterminated

[Code]
function PodmanInstalled: Boolean;
begin
  Result := FileExists(ExpandConstant('{pf64}\RedHat\Podman\podman.exe')) or
            FileExists(ExpandConstant('{pf}\RedHat\Podman\podman.exe'));
end;
