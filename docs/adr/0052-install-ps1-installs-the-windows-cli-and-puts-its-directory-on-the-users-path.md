# install.ps1 installs the Windows CLI and puts its directory on the user's PATH

Each release publishes two more assets, both listed in `SHA256SUMS`:
`koko-windows-amd64.exe`, the CLI for Windows, and `install.ps1`. The line
that installs koko on Windows is

```
irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex
```

install.ps1 needs no administrator. It reads the release from
`KOKO_VERSION` or the `releases/latest` redirect, downloads the binary and
`SHA256SUMS`, checks the SHA-256, and installs
`%USERPROFILE%\.local\bin\koko.exe` (ADR 0051). When that directory is not
in the user's PATH, the value Windows keeps under `HKCU\Environment`, it
adds it there and says so, the Owner's choice of 2026-11-11. install.sh
only prints the line (ADR 0047), because on Linux and macOS putting a
directory on PATH means editing a shell file the user wrote, and most
Linux distributions put `~/.local/bin` there already. On Windows nothing
puts it there, the user's PATH is a setting the system keeps for the
purpose and not a file of the user's, and installers are expected to set
it. The uninstall steps take it out again.

Windows does not let a running program's file be replaced, but lets it be
renamed within its directory. So `koko update` and install.ps1 rename the
running `koko.exe` aside, as `.koko.old-<random>.exe`, rename the checked
file into place, and delete the old file once no process runs it.

This refines ADR 0026: the binary's name is `koko-<os>-<arch>`, with
`.exe` on Windows, and install.ps1 joins the contract, so every later
install.ps1 installs every earlier release that has a Windows binary. It
refines ADR 0047: on Windows `koko update` replaces
`%USERPROFILE%\.local\bin\koko.exe`, by the rename aside, and the install
script writes PATH.

## Considered Options

- **Printing the PATH line, as install.sh does:** one rule for both
  scripts, but every person on Windows would have to act before `koko`
  is found.
- **A `.zip` archive:** what many Windows downloads are, but a release has
  one file to install, as ADR 0026 found for `.tar.gz`.
- **winget, scoop or an MSI as the first way to install:** familiar on
  Windows, but each is a second place to publish every release, and none
  serves an agent's box better than one line.
- **A native arm64 binary now:** Windows on Arm runs the x64 one under
  emulation, so it waits for someone who needs it.
- **Signing the binary:** no SmartScreen prompt for a file a browser
  downloaded, but a certificate to buy and keep. A file the install line
  downloads carries no mark of the Internet, and meets no prompt.

## Consequences

- A release from before the Windows CLI lists no `koko-windows-amd64.exe`;
  install.ps1 refuses it, naming the first release that has one.
- Between the two renames there is no `koko.exe`: a command started in
  that instant fails as not found.
- An old `koko.exe` that still runs, such as a Device Mux, stays beside
  the new one, hidden, until a later `koko update` or install.ps1 deletes
  it.
- install.ps1 takes no arguments and installs no koko server, which runs
  on Linux only.
- install.ps1 never touches the machine's PATH, and removes no other
  `koko`.
