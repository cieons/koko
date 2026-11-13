# Installing koko

[中文](install.zh-CN.md)

koko is two programs from one release: `koko`, the CLI, on Linux, macOS and
Windows, and `kokod`, the koko server, on Linux. One line installs the CLI, and the
same line with `--owner` installs the koko server too, on one Linux machine
with systemd. Without systemd, the koko server runs in a container from its
image, `ghcr.io/cieons/koko` ([In a container](#in-a-container)). This page
covers installing them, checking a release, upgrading and going back,
keeping the Root Key, restoring onto another machine, and what to do
without the Root Key, then the same for the container, and last, serving
the web UI to browsers through a reverse proxy
([The web UI in a browser](#the-web-ui-in-a-browser)). How each step works
is in the [installer](subsystems/installer.md) and
[release](subsystems/release.md) references.

## What the machine needs

The koko server's machine:

- Linux on amd64 or arm64, with systemd 247 or later running it: Debian 11,
  Ubuntu 22.04, RHEL 9 or later. `koko server install` refuses a container
  that systemd does not run, before it downloads anything: run
  [the image](#in-a-container) there instead.
- `/var/lib/koko` on a local filesystem, not NFS or another network
  filesystem.
- A free TCP port, 7822, or the one given with `--port`.
- `curl`, `sha256sum`, `awk` and `mktemp`, and root, as the root user or
  through `sudo`.

A machine that runs the CLI alone, as a device:

- Linux on amd64 or arm64, or macOS on Apple Silicon.
- Windows 10 (1809 and later), Windows 11, or Windows Server 2019 and
  later, on x64. On Arm64, Windows 11 runs the same x64 CLI under
  emulation. The first release with a Windows CLI is v0.6.0.

## Installing

On the machine, run the following as root, naming the Owner and the
address devices will use to reach the koko server:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s -- --owner alice --address koko.example.com
```

As the root user, drop the `sudo`.

- **`--address`**: a DNS name is recommended. It is the Server Address every Join
  Code carries, and devices keep reaching the koko server through it when
  the machine's IP changes. Without `--address`, install takes the
  machine's public IP, and refuses a private one (10.x, 192.168.x,
  100.64.x and the like), since devices could not reach it. Behind NAT that
  maps a different port, include that port: `--address koko.example.com:8443`.
- **`--port`**: the port the koko server listens on, 7822 by default. Install
  never moves to another port by itself: when the port is taken, it names
  the process holding it and stops. A network that lets only web traffic
  out (ports 80 and 443) cannot reach 7822: choose `--port 443` when
  devices are behind such a network and no other program on the machine
  holds port 443.
- **`KOKO_VERSION`** pins a release instead of the latest:
  `curl … | sudo env KOKO_VERSION=v0.3.0 sh -s -- …`. `sudo` alone does
  not pass the variable on, so the line uses `env`.
- **A `/tmp` mounted `noexec`**: install.sh runs `koko`, and `koko` runs
  `kokod`, from a temporary directory, so point `TMPDIR` at one that allows
  running programs: as root, `curl … | TMPDIR=/var/tmp sh -s -- …`, and
  otherwise `curl … | sudo env TMPDIR=/var/tmp sh -s -- …`, since `sudo`
  does not pass `TMPDIR` on either.

install.sh downloads `koko` for the machine, checks it against the
release's `SHA256SUMS`, and runs it only when it matches, as
`koko server install` with the arguments given. That downloads `kokod` of the
same version, `kokod-linux-<arch>`, checks it against the same
`SHA256SUMS`, and runs it only when it matches. `kokod` installs itself as
`/usr/local/bin/kokod`, writes the systemd unit, `koko.service`, and starts
the koko server, which `koko` then initialises. Once that has succeeded,
install.sh installs `koko` as `/usr/local/bin/koko`. It prints:

- the Server Address and the host key's fingerprint;
- where the Root Key is and its id ([keep a copy](#keeping-the-root-key));
- the port to open: **open that TCP port in the provider's firewall**,
  since koko opens none itself;
- the first Join Code, which works once, within 15 minutes, and the
  `koko join` line to run with it.

On the Owner's computer, install the CLI ([below](#the-cli-alone)), then
run the printed `koko join koko_…` line. If the code is lost or has
expired, `koko device add` on the machine prints a new one.

### The CLI alone

The same line without arguments installs the CLI alone, on Linux (amd64 or
arm64) and macOS (Apple Silicon), for the user who runs it, without
`sudo`. Windows has a line of its own ([On Windows](#on-windows)):

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
```

It downloads `koko` for the machine, checks it against the release's
`SHA256SUMS`, and installs it as `~/.local/bin/koko`, making
`~/.local/bin` when it is missing. It needs neither root nor systemd, so
it also works in a container. It never prompts, writes only to stderr, and
edits no file outside `~/.local/bin`. `koko update` upgrades it later
([Upgrading](#upgrading)).

Most Linux distributions put `~/.local/bin` on `PATH` when it exists;
macOS does not. When it is not on `PATH`, install.sh prints the line that
puts it there for the shell that `SHELL` names, and the full path to run
`koko` by until a new shell starts. For zsh, the line is:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zprofile
```

For bash, it is the same line with `~/.bashrc` on Linux and
`~/.bash_profile` on macOS; for fish, `fish_add_path ~/.local/bin`; for
any other shell, the `export` line, for the shell's profile. When another
`koko` comes first on `PATH`, such as a `/usr/local/bin/koko` an earlier
release installed, install.sh names it ([From v0.3.x](#from-v03x)).

Run as root, the same line installs the CLI as `/usr/local/bin/koko`, for
every user of the machine who has none of their own, and
`sudo koko update` upgrades it. A user with `sudo` gets it with
`curl … | sudo sh`. An agent running as root gets this one; an agent
running as another user gets its own `~/.local/bin/koko`, whose full path
install.sh prints for a harness that does not read a login shell's
`PATH`.

Without install.sh, download the file for the machine, `koko-darwin-arm64`
for a Mac, and `koko-linux-amd64` or `koko-linux-arm64` for Linux, check it
and install it. For a Mac:

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-darwin-arm64" -O "$base/SHA256SUMS"
grep ' koko-darwin-arm64$' SHA256SUMS | shasum -a 256 -c
mkdir -p ~/.local/bin
install -m 0755 koko-darwin-arm64 ~/.local/bin/koko
```

For Linux on amd64:

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-linux-amd64" -O "$base/SHA256SUMS"
grep ' koko-linux-amd64$' SHA256SUMS | sha256sum -c
mkdir -p ~/.local/bin
install -m 0755 koko-linux-amd64 ~/.local/bin/koko
```

#### On Windows

In PowerShell, the following line installs the CLI for the user who runs
it, without administrator rights:

```powershell
irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex
```

From cmd, or from an agent's harness, run the same line through Windows
PowerShell:

```bat
powershell -NoProfile -Command "irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex"
```

install.ps1 runs in Windows PowerShell 5.1, which every supported Windows
has, and in PowerShell 7. It downloads `koko-windows-amd64.exe`, checks it
against the release's `SHA256SUMS`, and installs it as
`%USERPROFILE%\.local\bin\koko.exe`, making the directory when it is
missing. It never prompts, writes its messages to the error stream and
nothing to the output stream, and prints the full path of `koko.exe`, for a
harness that does not read `PATH` again. It takes no arguments, since the
koko server runs on Linux only. `koko update` upgrades the CLI later
([Upgrading](#upgrading)).

When `%USERPROFILE%\.local\bin` is not in the user's `PATH`, install.ps1
adds it to that value, which Windows keeps for each user, and to the
`PATH` of the PowerShell session it ran in, and says what it added. It
never changes the machine's `PATH`. A terminal that was already open must
be reopened to find `koko`. When another `koko` comes first on `PATH`,
install.ps1 names it.

On Windows, `~` in this page and in koko's messages is the profile
directory, `%USERPROFILE%`, such as `C:\Users\alice`: the CLI is
`~\.local\bin\koko.exe`, and the device's files are in `~\.ssh\koko`.

Without install.ps1, as on a machine whose administrator does not allow
the line above, download `koko-windows-amd64.exe` and `SHA256SUMS`, check
the file and install it. In PowerShell:

```powershell
$base = 'https://github.com/cieons/koko/releases/latest/download'
Invoke-WebRequest "$base/koko-windows-amd64.exe" -OutFile koko-windows-amd64.exe -UseBasicParsing
Invoke-WebRequest "$base/SHA256SUMS" -OutFile SHA256SUMS -UseBasicParsing
$want = ((Select-String -Path SHA256SUMS -Pattern ' koko-windows-amd64\.exe$').Line -split ' ')[0]
(Get-FileHash koko-windows-amd64.exe -Algorithm SHA256).Hash -eq $want
```

The last line prints `True` when the file is the one `SHA256SUMS` lists.
Only then, install it:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.local\bin" | Out-Null
Move-Item koko-windows-amd64.exe "$env:USERPROFILE\.local\bin\koko.exe"
```

Then add `%USERPROFILE%\.local\bin` to the user's `PATH`, in Windows'
settings under "Edit environment variables for your account". The file is
not signed, so one downloaded with a browser may meet a Windows SmartScreen
prompt when it is first run; one downloaded by install.ps1 or by the lines
above does not.

#### ssh on Windows

- **OpenSSH for Windows is the supported `ssh`**: the client Windows
  provides as an optional feature, which PowerShell, cmd and Windows
  Terminal run. `ssh web1`, `scp` and `sftp` reach a Host through koko with
  it, as on Linux and macOS.
- **Git for Windows' `ssh`**, the one Git Bash runs, reads the same files
  and also works.
- **A Windows without the OpenSSH client** can still join: `koko join`,
  `koko exec`, `koko web` and the record commands need no `ssh`. `koko
  status` says when no `ssh` is on `PATH`. To add the client, run the
  following in PowerShell as an administrator:

  ```powershell
  Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0
  ```

- **`rsync`** is in neither client's distribution. On Windows, copy files
  with `scp` and `sftp`.
- **`HOME`**: Windows does not set it, and koko writes its files under the
  profile directory. When `HOME` is set to another directory, `ssh` may
  read another configuration than koko's; `koko status` says so, naming
  both directories: unset `HOME`, or set it to the profile directory.
- **WSL** is a Linux machine with a home of its own, and its `ssh` does not
  read the files koko wrote on Windows. Install the Linux CLI inside WSL,
  with the `curl` line above, and join it there with a Join Code of its
  own: it is a device of its own.

#### Windows terminals

- **A Secret in Git Bash.** Git Bash's own window is not a console to a
  Windows program, so koko cannot prompt for a Secret there. Give the
  Secret on stdin or in a file, as `koko secret add <space>/<secret>
  --from-file <file>` does, or run the command in PowerShell, cmd or
  Windows Terminal, where the prompt reads it without showing it.
- **Piped output in PowerShell.** koko writes UTF-8. Text printed to the
  console shows correctly as it is. Output that PowerShell captures or
  pipes is decoded by `[Console]::OutputEncoding`, in Windows PowerShell
  5.1 in particular, so set it first when a Host name or a value may hold
  characters outside ASCII:

  ```powershell
  [Console]::OutputEncoding = [Text.Encoding]::UTF8
  ```

- **Quoting for `koko exec`.** `koko exec` joins its words with spaces, as
  `ssh` does, and the Host's shell reads the result. PowerShell and cmd
  read the line first, each by its own rules. To pass quotes on to the
  Host's shell, put the whole command in double quotes and use single
  quotes inside it. In PowerShell:

  ```powershell
  koko exec web1 "echo 'a  b'"
  ```

  In cmd:

  ```bat
  koko exec web1 "echo 'a  b'"
  ```

  Either way the Host's shell runs `echo 'a  b'`. Windows PowerShell 5.1
  drops a double quote inside an argument on the way to any program, and
  single quotes group nothing in cmd, so the other ways round do not
  arrive as written.

### Offline

On a machine that cannot reach GitHub, `koko server install` cannot
download `kokod`: it says so, naming `--kokod`, and writes nothing. On
such a machine, install the koko server from copied files. On a machine
with access to GitHub, download `koko-linux-<arch>`, `kokod-linux-<arch>`
and `SHA256SUMS` from one release, for the target machine's architecture,
amd64 below:

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-linux-amd64" -O "$base/kokod-linux-amd64" -O "$base/SHA256SUMS"
```

Copy the three files onto the target machine. There, in the directory
that holds them, check them, then install the koko server and the CLI:

```sh
grep -E ' kokod?-linux-amd64$' SHA256SUMS | sha256sum -c
chmod 0755 koko-linux-amd64 kokod-linux-amd64
sudo ./koko-linux-amd64 server install --kokod ./kokod-linux-amd64 --owner alice --address koko.example.com
sudo install -m 0755 koko-linux-amd64 /usr/local/bin/koko
```

`koko server install --kokod` reads the file's build without running it,
and runs the file only when it is the same build as the `koko` running it:
a `kokod` from another release is refused, naming both versions, and
nothing runs. It runs the file in place, so the file must be executable,
as the `chmod` above makes it. It prints what the install line prints.

## Checking a release

Every file of a release is attested by the GitHub workflow that built it.
With the [GitHub CLI](https://cli.github.com), check that a downloaded file
was built by koko's release workflow:

```sh
gh attestation verify kokod-linux-amd64 --repo cieons/koko
```

This works for every file `SHA256SUMS` lists: `koko-<os>-<arch>`
(`koko-windows-amd64.exe` for Windows), `kokod-linux-<arch>`, `install.sh`
and `install.ps1`. The koko server's machine needs no `gh`: install.sh,
install.ps1 and `koko server install` check the SHA-256 alone.

## Upgrading

The CLI upgrades itself, without root:

```sh
koko update
```

It downloads the latest release's `koko` for the machine, checks it
against the release's `SHA256SUMS` and its build, and replaces the file it
runs from. Open SSH Sessions, `koko web` and running commands carry on.
`koko update --check` says what it would do and changes nothing. Root's
`/usr/local/bin/koko` upgrades with `sudo koko update`.

On Windows, `koko update` upgrades `~\.local\bin\koko.exe`, the file
install.ps1 installs, whether or not it runs as an administrator, and
refuses a `koko.exe` anywhere else. Windows does not let a running
program's file be replaced, so `koko update` renames the old file aside,
as `.koko.old-<random>.exe` in the same directory, and puts the new one in
its place: open SSH Sessions and running commands carry on from the old
file, which a later `koko update` or install.ps1 deletes once nothing runs
it. The install line, run again, upgrades the CLI the same way.

The koko server, installed with systemd, upgrades in place with one
command, run by the Owner or an Administrator on any of their devices, or
on its own machine:

```sh
koko server upgrade            # to the latest release
koko server upgrade --check    # what it would upgrade to; changes nothing
```

`koko server upgrade` asks the koko server for the upgrade, and the koko
server asks its upgrade helper, `koko-upgrade.service`, which runs as root.
The helper takes nothing from the koko server but the version of a release
newer than the one installed. It downloads that release's `kokod` itself,
checks it against the release's `SHA256SUMS` and its build, keeps the
`kokod` installed as `/var/lib/koko-upgrade/kokod.previous`, and installs
the new one, which restarts the koko server. Every open SSH Session through
it ends. When the new `kokod` does not start, the helper stops it, puts the
database's snapshot back when it had changed the schema, and installs the
previous `kokod` again. `koko server upgrade` waits for the outcome, then
brings the CLI that ran it to the same release when `koko update` can
replace it, or says how to. On failure it exits with `upgrade_failed` and
the reason, and leaves the CLI as it was. The web UI offers the Owner and
the Administrators the same upgrade: a notice above every page says when a newer release is out,
and its Upgrade button, after a confirmation, asks for the upgrade and
follows it until the koko server runs the new release, then reloads the
page, or says why it failed and which version runs. Check now, in the
notice and in the account panel, checks for a release at once, as
`koko server upgrade --check` does. `koko server show` shows the outcome
of the last upgrade, and
`journalctl -u koko -u koko-upgrade` on the machine shows the details.

The install line, run again as root without arguments, also upgrades the
koko server, and installs the upgrade helper on a koko server that lacks
it:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh
```

It upgrades the koko server first, then root's CLI. `koko server install`
replaces `kokod` with the new release's, writes the systemd unit again and
restarts the koko server, which keeps its record, its Owner, its Server
Address and its keys. Every open SSH Session ends, with a line saying the
koko server is restarting. Only then does install.sh replace `koko`. When
the koko server's upgrade fails, install.sh stops there and leaves the CLI
as it was, so the CLI never runs ahead of the koko server beside it. After
`koko server upgrade`, the koko server may run a newer release than root's
CLI beside it, which works. `koko server install` then refuses to put the
older `kokod` of that CLI in place of the newer one, and names `sudo koko
update`, which brings the CLI up first ([Going back](#going-back)).

Run on the koko server's machine without `sudo`, the line installs the
CLI of the user who runs it, in `~/.local/bin`, and leaves root's
`/usr/local/bin/koko` as it was: run it with `| sudo sh` there. A user who
can use `sudo` without a password still upgrades the koko server, through
`sudo`. For a user who can do neither, the line without arguments leaves
the koko server as it is and says so, naming `koko server upgrade`; with
arguments, it is refused, and nothing is written.

A koko server installed by v0.1.0 or v0.2.0, from before `koko` and `kokod`
were two programs, takes the same upgrade: its unit then runs
`/usr/local/bin/kokod run`, and `/usr/local/bin/koko` is the CLI.

`--owner` and `--address` are refused on an upgrade: change the Server
Address with `koko server set address=<host[:port]>` instead.

The upgrade keeps the port the koko server listens on, even one set with a
drop-in (`systemctl edit koko`); after a drop-in moves it below 1024, run
the install line again, which lets the koko server bind it. `--port` moves
it. When the Server Address names no port of its own, devices follow the
listen port, so after a move the output names the `koko connect
<host:port>` line to run on each device.

When the new koko changes the database's schema, the koko server first
copies the database as it was to `/var/lib/koko/koko.db.before-<n>`, the
snapshot, and keeps only the latest one.

### From v0.3.x

Up to v0.3.x, the CLI line installed `/usr/local/bin/koko`, as root. On a
device with that file, in this order:

1. Run the CLI line without `sudo` ([The CLI alone](#the-cli-alone)). It
   installs `~/.local/bin/koko`, then names `/usr/local/bin/koko`, which it
   leaves where it is, and gives the line that removes it.
2. When `command -v koko` in a new shell still names
   `/usr/local/bin/koko`, put `~/.local/bin` before `/usr/local/bin` on
   `PATH`, with the line install.sh prints when `~/.local/bin` is missing
   from it.
3. Run any `koko` command, such as `koko status`, with the new `koko`. It
   points koko's ssh_config at itself, so `ssh <host>.koko` runs it too.
4. Once no other user of the machine runs it, remove the old file:

   ```sh
   sudo rm /usr/local/bin/koko
   ```

To keep one `koko` for every user of the machine instead, run the line
with `| sudo sh`: it replaces `/usr/local/bin/koko`, as before, and
`sudo koko update` upgrades it from then on.

On a v0.3.x koko server with systemd, run the line as root, as above. It
upgrades the koko server and installs the upgrade helper, so that `koko
server upgrade` and the web UI's upgrade work from then on. The CLI there
stays root's `/usr/local/bin/koko`.

## Going back

To go back with the CLI, name the release:

```sh
koko update --version v0.3.0
```

with that release in place of `v0.3.0`. A release from before
`koko update`, such as v0.3.0, has no `koko update`: to come back up from
it, run the install line again ([The CLI alone](#the-cli-alone)).

On Windows, `koko update --version` goes back the same way, and so does
install.ps1 with the release named in `KOKO_VERSION`, in PowerShell:

```powershell
$env:KOKO_VERSION = 'v0.6.0'; irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex
```

with that release in place of `v0.6.0`. The variable stays set in that
PowerShell session, where a later install line would install the same
release again: `Remove-Item Env:KOKO_VERSION` clears it. v0.6.0 is the
first release with a Windows CLI: an older release is refused, naming
v0.6.0, and nothing is written.

To go back on the koko server's machine to the release that ran before the
upgrade, install it with the newest install.sh, naming the release in
`KOKO_VERSION`. On the machine, as root:

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | env KOKO_VERSION=v0.3.0 sh
```

with that release in place of `v0.3.0`. The release's own install.sh, at
`releases/download/v0.3.0/install.sh`, also works, with the same
`KOKO_VERSION`. `koko server upgrade` and the upgrade helper only ever go
forward, so going back is always this line. When the upgrade changed no schema, nothing more is
needed. When it did, the older koko refuses to install over the newer
database and writes nothing, while the newer koko server keeps running, and
install.sh leaves the CLI as it was. Its refusal names the snapshot to put
in place:

```text
koko: /var/lib/koko/koko.db: it is at schema version 14, which this koko (schema version 13) predates: run the newer koko that migrated it, or go back by putting koko.db.before-14 in place of koko.db; nothing was written
```

Put it in place, then install the older release again:

```sh
systemctl stop koko
cd /var/lib/koko
mkdir /var/lib/koko-newer
mv koko.db /var/lib/koko-newer/
mv koko.db-wal koko.db-shm /var/lib/koko-newer/ 2>/dev/null # there only after a crash
mv koko.db.before-<n> koko.db
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | env KOKO_VERSION=v0.3.0 sh
```

with the snapshot the refusal named in place of `koko.db.before-<n>`, and
the same version as above in place of `v0.3.0`. Anything written after the
upgrade is lost with the newer database, which stays in
`/var/lib/koko-newer`.

`koko server install` refuses to replace a `kokod` that is a newer release
than its own CLI, as after `koko server upgrade`, so that changing a setting
with an older CLI never takes the koko server back. It names both versions
and `sudo koko update`, and writes nothing. `KOKO_VERSION` naming the CLI's
own release lets it through, which is how the lines above go back:
install.sh passes `KOKO_VERSION` on to `koko server install`. Run by hand,
the same is `sudo env KOKO_VERSION=v0.3.0 koko server install`.

Going back to v0.2.0 or v0.1.0, from before `koko` and `kokod` were two
programs, takes one more line, with or without a snapshot. The older koko
installs its own binary and unit, and leaves `/usr/local/bin/kokod` behind,
unused. Once the older koko server runs, remove it:

```sh
rm /usr/local/bin/kokod
```

## Keeping the Root Key

`/etc/koko/root.key` seals every Secret and the koko server's own keys.
Without it, nothing sealed can be opened again, so **keep a copy off the
machine**, such as in a password manager. The file is one line of text
that names its own id, `rootkey_…`, which `koko server show` also prints:

```sh
sudo cat /etc/koko/root.key
```

A copy of `/var/lib/koko` without the Root Key reveals no Secret and no
private key, so it is safe to keep anywhere; a [restore](#restoring-onto-another-machine)
needs both.

## Restoring onto another machine

koko has no backup command yet. A copy of the data directory taken while the
koko server is stopped, kept with the Root Key, restores it:

1. On the old machine: `systemctl stop koko`, copy `/var/lib/koko` as a
   whole directory, and keep it with the copy of the Root Key: a restore
   needs both.
2. On the new machine, as root, put both back: the directory at
   `/var/lib/koko`, and the Root Key at `/etc/koko/root.key`, owned by root
   with mode 0600, in `/etc/koko`, mode 0700.
3. Install the CLI as root, with the line without arguments piped to
   `sudo sh` ([The CLI alone](#the-cli-alone)), then run
   `sudo koko server install` without `--owner`, adding `--port <port>` if
   the old koko server listened on another port than 7822. It finds the
   koko server's record, installs the `kokod` of its own version and
   starts it.

The host key travels in the data directory, so devices still trust the koko
server. If its address changed, set the new one with
`koko server set address=<host[:port]>`, and run
`koko connect <host:port>` on each device.

systemd gives the data directory and everything in it to the `koko` user
when the directory itself belongs to someone else, as a copy put back by
root does. If the files were copied into a `/var/lib/koko` that already
belongs to `koko`, run `chown -R koko:koko /var/lib/koko` before installing.

## Without the Root Key

If the Root Key and every copy of it are lost, the record cannot be opened
again. The only remaining option is to start over: move `/var/lib/koko`
aside, along with any `/etc/koko/root.key` that does not load, and install
again with `--owner`. That costs everything sealed and everything joined: every Host
is added and Enrolled anew, every Secret is added again, and every device
runs `koko leave`, then joins with a new Join Code.

## Uninstalling

`koko server uninstall` stops the koko server and removes its unit, the
upgrade helper's units and its directory, `/var/lib/koko-upgrade`, and
`/usr/local/bin/kokod`, so that nothing of koko is left running as root.
It keeps the CLI, `/usr/local/bin/koko`, the data directory, the Root Key
and the `koko` user, and prints the port it
listened on, and the web listener's address when it had one. To run the
koko server again on the same data, run `sudo koko server install` without
`--owner`, with that `--port`, and that `--web-listen` when it printed
one: it installs the `kokod` of its own version again.

The CLI is one file: remove it with `rm ~/.local/bin/koko`, or with `sudo
rm /usr/local/bin/koko` for root's.

On Windows, remove the CLI in PowerShell, in this order:

1. Leave the koko server, which stops every Device Mux and removes the
   join:

   ```powershell
   koko leave
   ```

2. Delete `koko.exe`, and any older one `koko update` left beside it:

   ```powershell
   Remove-Item -Force "$env:USERPROFILE\.local\bin\koko.exe", "$env:USERPROFILE\.local\bin\.koko.old-*.exe"
   ```

3. Take the directory out of the user's `PATH`, unless another tool uses
   `%USERPROFILE%\.local\bin`:

   ```powershell
   $key = Get-Item HKCU:\Environment; Set-ItemProperty HKCU:\Environment -Name Path -Type $key.GetValueKind('Path') -Value (($key.GetValue('Path', '', 'DoNotExpandEnvironmentNames') -split ';' | Where-Object { $_ -and [Environment]::ExpandEnvironmentVariables($_).Trim('"').TrimEnd('\') -ne "$env:USERPROFILE\.local\bin" }) -join ';')
   ```

   The line keeps every other entry as it was written, and the value's
   type. Terminals opened after the next sign-in no longer have the
   directory on `PATH`.

4. Delete koko's runtime directory, then delete the line
   `Include ~/.ssh/koko/ssh_config` from `~\.ssh\config`:

   ```powershell
   Remove-Item -Recurse -Force "$env:LOCALAPPDATA\koko"
   ```

## In a container

The image `ghcr.io/cieons/koko` runs the koko server where systemd does
not, such as alongside other containers in Docker. Each release pushes
it for amd64 and arm64, holding both of that release's programs:
`kokod-linux-<arch>` as `/usr/local/bin/kokod` and `koko-linux-<arch>` as
`/usr/local/bin/koko`. It runs the koko server as `kokod run`, so a Compose
file that sets no `entrypoint` and no `command`, as the one below, needs
only its `KOKO_VERSION` changed from release to release. The container
runs as uid 65532 from the start, never as root, with no capability, and
koko changes no file's owner. The reasons are recorded in
[ADR 0037](adr/0037-the-container-keeps-its-data-and-its-root-key-in-one-directory.md),
[ADR 0038](adr/0038-a-container-initialises-at-its-first-start-and-gives-its-first-join-code-to-docker-exec.md)
and [ADR 0039](adr/0039-the-container-runs-as-uid-65532-from-the-start-and-koko-changes-no-owner.md).

### What the Docker host needs

- Docker with Compose v2 (`docker compose`), on amd64 or arm64.
- A folder dedicated to koko, `/srv/koko` below, writable by the current
  user (`sudo install -d -o "$USER" /srv/koko`), on a local filesystem,
  not NFS or another network filesystem: the koko server refuses one at start. It
  holds the whole deployment: the Compose file, its `.env`, and `state/`,
  everything the container keeps.
- A free TCP port to publish, 7822 below. **Open it in the provider's
  firewall**, since koko opens none itself.

No user and no key are prepared on the Docker host, only `state/`, once,
before the first start, owned by uid 65532 with mode 0700, so that on the
Docker host only root can read it. In `/srv/koko`:

```sh
sudo install -d -o 65532 -g 65532 -m 0700 state
```

Without it, Docker makes `state/` as root, mode 0755, and the koko server
refuses to start, writing nothing, naming the directory and the uid and
gid 65532 it runs as. To prepare nothing at all, use a named volume
instead, `koko:/koko` in place of `./state:/koko` and
`volumes: {koko: {}}` at the end of the file: Docker fills a new one from
the image's `/koko`, which is uid 65532's already, and keeps it under its
own directory (`docker volume inspect`). The commands below run in
`/srv/koko` and take `state/`.

### Starting it

Save [`release/compose.yaml`](../release/compose.yaml) as `compose.yaml` in
`/srv/koko`:

```yaml
# The koko server in a container: see docs/install.md, "In a container".
# Keep this file in a folder of its own, such as /srv/koko, and there,
# before the first start:
#   echo KOKO_VERSION=0.4.2 > .env    # the release to run, without its v
#   sudo install -d -o 65532 -g 65532 -m 0700 state    # what koko keeps
# After it: docker exec koko koko device add    # your first Join Code
# To upgrade, set the new release in .env, then run
#   docker compose pull && docker compose up -d
# KOKO_VERSION=latest is allowed too: the pull and up alone then upgrade,
# going back means pinning a version again, and a release that changes the
# database migrates it at the first start after the pull that brings it.
# up -d alone never pulls, so nothing upgrades by surprise. A pinned
# version is the default, so that every upgrade is chosen.
services:
  koko:
    image: ghcr.io/cieons/koko:${KOKO_VERSION:?set KOKO_VERSION in .env, such as KOKO_VERSION=0.4.2}
    container_name: koko
    restart: unless-stopped
    stop_grace_period: 30s
    # uid 65532 from the start, with no capability and none to gain.
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
      # - "127.0.0.1:7823:7823"            # the web UI, for a reverse proxy on this machine alone
    volumes:
      - ./state:/koko      # the data and the Root Key, beside this file: make it with install -d (above), or use a named volume
    environment:
      KOKO_OWNER: alice                  # read only at the first start
      KOKO_ADDRESS: koko.example.com     # host[:published port]; read only at the first start
      # KOKO_WEB_LISTEN: 0.0.0.0:7823      # the web UI: see docs/install.md, "The web UI in a browser"
```

Set `KOKO_OWNER` to the Owner's User name, and `KOKO_ADDRESS` to the
address devices will use to reach the koko server: a DNS name is
recommended, as for [`--address`](#installing). Then, beside the file:

```sh
echo KOKO_VERSION=0.4.2 > .env     # the release to run, without its v
sudo install -d -o 65532 -g 65532 -m 0700 state   # once, unless a named volume is used
docker compose up -d
docker exec koko koko device add   # your first Join Code
```

`KOKO_VERSION` is required, so the container always runs a release
chosen explicitly. It may also be `latest`, the newest release: upgrading
is then `docker compose pull && docker compose up -d` with nothing edited,
and `docker compose up -d` alone never pulls, so nothing upgrades by
surprise. Going back then means pinning a version again, and a release
that changes the database's schema migrates it at the first start after
the pull that brings it. A pinned version stays the default, so that every
upgrade is chosen. At its first start the koko server makes the Root Key,
`state/root.key`, and initialises itself. `docker logs koko` then says
`koko server initialised`, with the host key's fingerprint, the listen
address, the Root Key's path and id, and the `koko device add` line to run
next. No log line holds the Root Key or a Join Code. A fresh koko server
without `KOKO_ADDRESS` refuses to start, naming it: the container's own IP
is private, and no device could reach it.

`koko device add` prints the first Join Code, which works once, within 15
minutes, and the `koko join` line to run with it on the Owner's computer
([The CLI alone](#the-cli-alone)). If the code is lost or has expired, run
the same line again, as after losing every device.

Without Compose, the same container with `docker run`, in `/srv/koko`:

```sh
docker run -d --name koko --restart unless-stopped --stop-timeout 30 \
  --cap-drop ALL --security-opt no-new-privileges:true \
  -p 7822:7822 -v "$PWD/state:/koko" \
  -e KOKO_OWNER=alice -e KOKO_ADDRESS=koko.example.com \
  ghcr.io/cieons/koko:0.4.2
```

### Owner commands

`docker exec koko koko <command>` runs the koko CLI inside the container,
as the image's uid 65532, where it reaches the koko server over its Unix
socket as the Local caller, which acts as the Owner. So the Owner needs no
device to act:

```sh
docker exec koko koko server show
docker exec koko koko server set address=<host[:port]>
docker exec koko koko device add
docker exec koko koko user invite bob
```

The Docker host's own shell is not a Local caller, since the socket stays
inside the container: from the Docker host, use `docker exec`, or join it
as a device. Mounting the container's `/run/koko` onto the Docker host is
not supported.

### The port

The container listens on 7822, so any other port, 443 included, is only a
port mapping on the Docker host. When the published port is 7822,
`KOKO_ADDRESS: koko.example.com` is enough. When it differs, name the
published port in the address, since it is the port devices dial:

```yaml
    ports:
      - "443:7822"
    environment:
      KOKO_ADDRESS: koko.example.com:443
```

`KOKO_OWNER` and `KOKO_ADDRESS` are read only at the first start. A later
start whose values differ from the record logs a warning and changes
nothing. To move the published port later, change the mapping and
`KOKO_ADDRESS`, run `docker compose up -d`, then
`docker exec koko koko server set address=koko.example.com:<port>`, and
`koko connect koko.example.com:<port>` on each device.

With `network_mode: host`, the container's port is the machine's: leave
out `ports:`, and set `KOKO_LISTEN` (such as `:8443`) to listen on another
port than 7822. It must be 1024 or above, since the koko server holds no
capability. When `KOKO_ADDRESS` names no port, devices dial the one the
koko server listens on.

### Backups and the Root Key

`state/` holds everything the container keeps: the Root Key,
`state/root.key`, one line that names its own id, and the data directory,
`state/data/`, with `koko.db` and the latest `koko.db.before-<n>` snapshot.

The Root Key seals every Secret and the koko server's own keys. A copy of
`state/data/` alone reveals no Secret and no private key. **A copy of the
whole of `state/` holds the record and its key together**, and the koko
process can read, change or delete `root.key`, which systemd prevents on
an installed koko server. So:

1. Right after the first start, copy `root.key` off the machine, such as
   into a password manager. It is one line: `sudo cat state/root.key`.
   `docker exec koko koko server show` prints its id.
2. Back up `state/data/` on its own, copied as root while the container is
   stopped (`docker compose stop`, then `docker compose start`). That copy
   can be kept anywhere.
3. Keep a copy of the whole of `state/` only where the Secrets themselves
   would be kept.
4. A disk snapshot of the machine holds both, but the provider already
   controls the machine.

The koko server never makes a Root Key over sealed data: a missing or wrong
`root.key` is refused at start, naming the id the data needs. Without any
copy of it, the way on is the one in
[Without the Root Key](#without-the-root-key): move `state/data/` aside and
start again.

### Upgrading the container

The web UI tells the Owner and the Administrators when a newer release is
out, in a notice above
every page, and its How to upgrade shows the two steps, each with Copy;
`koko server show` names them too. Set the new release in `.env`, such as
`KOKO_VERSION=0.5.0`, then, beside `compose.yaml`:

```sh
docker compose pull && docker compose up -d
```

With `KOKO_VERSION=latest`, that line alone upgrades. Compose pulls the
new image and replaces the container, which keeps its
record, its Owner, its Server Address and its keys in `state/`. Every
open SSH Session ends, with a line saying the koko server is restarting.
When the new koko changes the database's schema, the koko server first
copies the database as it was to `state/data/koko.db.before-<n>`, and
keeps only the latest snapshot.

With `docker run`, run `docker stop koko` and `docker rm koko`, which keep
`state/`, then the `docker run` line with the new version.

### Going back in a container

To go back to the release that ran before the upgrade, set its version in
`.env` and run `docker compose up -d`. When the upgrade changed no schema,
nothing more is needed. When it did, the older image refuses at start and writes nothing, and
`docker logs koko` names the version it needs and the snapshot to put in
place. Put it in place on the Docker host, in a root shell (`sudo -s`),
beside `compose.yaml`:

```sh
docker compose down
mkdir newer
mv state/data/koko.db newer/
mv state/data/koko.db-wal state/data/koko.db-shm newer/ 2>/dev/null # there only after a crash
mv state/data/koko.db.before-<n> state/data/koko.db
docker compose up -d
```

with the snapshot the refusal named in place of `koko.db.before-<n>`.
Anything written after the upgrade is lost with the newer database, which
stays in `newer/`.

### Restoring or moving the container

1. On the old machine: `docker compose down`, then copy `/srv/koko` as a
   whole folder, as root, so the Compose file and `.env` travel with
   `state/`. Alternatively, copy `state/data/` alone and keep it with the
   copy of `root.key`: a restore needs both.
2. On the new machine, put them back in `/srv/koko`: the whole folder, or
   `compose.yaml`, `.env`, `state/data/` and `state/root.key`. Then give
   `state/` to uid 65532, as root, in `/srv/koko`, since koko changes no
   owner: `chown -R 65532:65532 state` and `chmod 0700 state`.
3. Run `docker compose up -d` there, with the same `compose.yaml` and
   `.env`.

The host key travels sealed in `state/data/`, so devices still trust the koko
server. If its address changed, run
`docker exec koko koko server set address=<host[:port]>`, and
`koko connect <host:port>` on each device.

### Moving from systemd

`koko server uninstall` stops an installed koko server and keeps its data
directory and Root Key. Put them in the container's `state/`, one that
holds no `data/` yet, as root, in `/srv/koko`:

```sh
koko server uninstall
install -d -o 65532 -g 65532 -m 0700 state
cp -a /var/lib/koko state/data
cp /etc/koko/root.key state/root.key
chown -R 65532:65532 state
```

Then start the Compose file as above, without `docker exec … device add`:
the devices already joined keep working. Set `KOKO_OWNER` and `KOKO_ADDRESS`
to its Owner and Server Address, or each start logs a warning that they
differ. If the installed koko server listened on another port than 7822,
publish that port (`"443:7822"` for 443), and, when its Server Address
names no port, name it, in `KOKO_ADDRESS` and with
`docker exec koko koko server set address=koko.example.com:443`.

Going the other way, put `state/data/` at `/var/lib/koko` and
`state/root.key` at `/etc/koko/root.key`, then follow
[Restoring onto another machine](#restoring-onto-another-machine) from
step 2, with `--port` set to the published port when it was not 7822.

### Checking the image

Each image carries a GitHub artifact attestation, which the
[GitHub CLI](https://cli.github.com) checks:

```sh
gh attestation verify oci://ghcr.io/cieons/koko:0.4.2 --repo cieons/koko
```

The image's `/usr/local/bin/kokod` and `/usr/local/bin/koko` are the
release's `kokod-linux-<arch>` and `koko-linux-<arch>` byte for byte, the
files `SHA256SUMS` lists.

### Kubernetes and other users

The image's user is the number 65532, so Kubernetes' `runAsNonRoot`
accepts it as it is; make its volume writable by uid 65532, such as with
`fsGroup: 65532`. A directory the koko server cannot write is refused at
start, naming the directory and the uid and gid it runs as, and the koko
server refuses to run as root, so never pass `--user 0` or `user: root`.
Another uid works only where `/koko` and `/run/koko` are writable by it,
such as with a tmpfs on `/run/koko` owned by it.

### The Root Key kept apart

To keep the Root Key out of the backed-up directory, mount it read-only
from elsewhere and point `KOKO_ROOT_KEY_FILE` at it. After the first start,
run `docker compose down`, then, as root, move `state/root.key` into a
directory of its own, such as `/etc/koko-key/root.key` (`mkdir /etc/koko-key`
first), still uid 65532's, and `chmod 0400` it. Then add
`/etc/koko-key/root.key:/run/secrets/koko-root.key:ro` to `volumes:` and
`KOKO_ROOT_KEY_FILE: /run/secrets/koko-root.key` to `environment:`, and run
`docker compose up -d`. The koko server only reads it; one it cannot read
is refused at start.

## The web UI in a browser

The web UI is reached in two ways. `koko web` serves it to the person
running it, on a joined device or on the koko server's machine, and needs
nothing more. A person who does not use the CLI, such as a teammate who
records Hosts from a phone, signs in instead at the **Web URL**, with
their User name and a Password. For that, the koko server serves the web
UI over plain HTTP on a second listener, the web listener, to a reverse
proxy such as Caddy, which holds a domain and its TLS certificate, since
koko does no TLS itself
([ADR 0042](adr/0042-the-web-ui-is-also-served-over-plain-http-to-the-owners-reverse-proxy.md)).

**Never expose the web listener directly.** It serves plain HTTP, so a
Password sent to it would cross the network unencrypted. It also takes the
client's address from the last entry of the `X-Forwarded-For` header,
which the proxy appends, so a client that reached it directly could choose
the address the per-address sign-in limit counts; only the limit for each
name would still hold. Bind it to the loopback address, as below, and do
not open its port in the provider's firewall.

### Turning the listener on

The web listener is off until it is given an address. On a machine
installed with systemd, give it to `koko server install` with
`--web-listen`, on the koko server's machine, as root:

```sh
sudo koko server install --web-listen 127.0.0.1:7823
```

This upgrades the koko server in place to the CLI's build, as the install
line does, keeping its record, and restarts it with the web listener on
`127.0.0.1:7823`, where only a proxy on the same machine reaches it. Later
upgrades keep the address; `--web-listen off` turns the listener off again.
Install refuses a port another process holds, naming it.

In a container, the listener takes every address of the container, and the
port mapping publishes it on the Docker host's loopback address alone.
In `compose.yaml` ([Starting it](#starting-it)), remove the `# ` that
starts each of the two lines naming port 7823, so that they read:

```yaml
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
      - "127.0.0.1:7823:7823"            # the web UI, for a reverse proxy on this machine alone
    environment:
      KOKO_WEB_LISTEN: 0.0.0.0:7823      # the web UI: see docs/install.md, "The web UI in a browser"
```

Then run `docker compose up -d`. With `docker run`, add
`-p 127.0.0.1:7823:7823 -e KOKO_WEB_LISTEN=0.0.0.0:7823`. When the proxy
runs in a container too, on a Docker network shared with the `koko`
container, the port need not be published: the proxy reaches the listener
at `koko:7823`.

At each start, the koko server logs a `web listener` line naming the
listener's address and the Web URL (`journalctl -u koko`, or
`docker logs koko`), and `koko server show` names both.

### The reverse proxy

With Caddy, add a site for the domain to the Caddyfile:

```caddyfile
koko.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:7823
}
```

Caddy obtains the domain's certificate, passes the `Host` header on as the
browser sent it, and sets `X-Forwarded-For` with the client's address as
its last entry, which is all koko needs. `encode zstd gzip` compresses the web UI's files,
which koko serves uncompressed. The web UI is served at the root of the
domain; serving it under a path, such as `https://example.com/koko`, is not
supported.

The web terminal runs over a WebSocket, which Caddy proxies with no further
configuration. A reload of Caddy's configuration closes the WebSockets it
proxies, unless its `stream_close_delay` is set
([Caddy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy#stream_close_delay)).
Each open terminal then shows Reconnecting for a moment and Resumes its SSH
Session, with nothing lost, so the setting is optional.

> **nginx** by default sends the upstream's address as `Host`, sets no
> `X-Forwarded-For`, and passes on no WebSocket. In the `http` block, add
> the usual map
> ([nginx](https://nginx.org/en/docs/http/websocket.html)):
>
> ```nginx
> map $http_upgrade $connection_upgrade {
>     default upgrade;
>     ''      close;
> }
> ```
>
> and in the `location` that proxies to the web listener:
>
> ```nginx
> proxy_http_version 1.1;
> proxy_set_header Host $host;
> proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
> proxy_set_header Upgrade $http_upgrade;
> proxy_set_header Connection $connection_upgrade;
> ```
>
> Without the `Host` line, the koko server answers every request with
> `421` and a page naming its Web URL. Without the `X-Forwarded-For` line,
> every browser has the proxy's address, so 10 failed attempts from anyone
> stop sign-ins from everyone for 15 minutes. Without the
> `proxy_http_version`, `Upgrade` and `Connection` lines, the web terminal
> never connects.
>
> nginx's other defaults need no change. Both ends of a web terminal send
> something at least every 15 seconds, well within nginx's 60-second read
> timeout, and the Files panel uploads in chunks of 512 KiB, within its
> 1 MiB limit on a request's body (`client_max_body_size`).

### Setting the Web URL

The Web URL is the `https://` address the proxy serves, with a port only
when it is not 443, and no path. On the koko server's machine:

```sh
koko server set web_url=https://koko.example.com
```

In a container: `docker exec koko koko server set web_url=https://koko.example.com`.
Only the koko server's machine sets it, as it sets the Server Address: a
device or a browser cannot. Until it is set, the web listener answers every
request with a page naming this command. The koko server then answers only
requests whose `Host` header names the Web URL's host and port. Changing
the Web URL takes effect at once: Password Links issued for the old
address stop working, and every browser signs in again.
`koko server set --unset web_url` stops browser access and keeps the
listener.

### Inviting a browser User

The Owner or an Administrator invites a person who will use koko in a
browser with a Password
Link instead of a Join Code:

```sh
koko user invite bob --web
```

This creates bob as a Member and prints a link,
`https://koko.example.com/password#…`, which works once, within 15
minutes. `--valid` sets from 1 minute to 7 days, such as `--valid 1d` for a
link that will be opened later; `koko device add` takes `--valid` too. In
the web UI, the same Invite is on the Access page: **Invite**, then **In a
browser** and **Valid for**. The Owner or an Administrator runs these on
any of their devices or in a browser signed in as them, and the Owner also
on the koko server's machine (`docker exec koko` in a container). bob becomes a Member of a Space as any Member does:
`koko space member add <space> bob`, or in the Invite itself.

Opened in a browser, the link shows the User name and asks for a Password:
at least 15 characters and at most 128, any characters allowed, other
than the User name itself. Once it is
set, the browser is signed in. From then on, bob signs in at
`https://koko.example.com` with the name `bob` and that Password.

### Resetting and removing a Password

A User who has lost their Password gets a new Password Link, as does a User
of the CLI who wants to use a browser too, the Owner included:

```sh
koko password link bob
```

It takes `--valid` as the Invite does, and voids bob's earlier link if it
was not used. Setting a Password through it signs out every browser
signed in as bob. In the web UI, it is **Issue Password Link** in bob's
panel on the Access page, where each person's row says whether they have
a Password.

To take browser access away and keep the User, their Spaces and their
devices:

```sh
koko password rm bob
```

This deletes the Password and signs out every browser signed in as bob; it
is **Remove Password** in bob's panel. `koko user remove bob` Removes bob
altogether, ending their browsers' Sign-ins with everything else, or
deletes bob when they never used koko.

### Sign-ins

Each browser signed in is a Sign-in. It ends when the browser signs out,
after 7 days unused, 30 days after it began, when the User's Password is
set or removed, or when it is revoked:

```sh
koko signin ls
koko signin revoke <id>
```

`koko signin ls` lists the caller's own Sign-ins, and every User's for the
Owner or an Administrator; `--all` adds the ended ones. In the web UI, each person sees their
own under **Browser Sign-ins** on the Account page, where **Change
Password** also changes their Password and signs out their other browsers.
The Owner or an Administrator sees a person's Sign-ins in that person's
panel on the Access
page, with **Revoke**.

After 10 wrong Passwords in a row for one name, sign-ins with that name
are refused for 15 minutes, or until a Password is set through a Password
Link. After 10 failed attempts from one address within 15 minutes, that
address is refused until the oldest is 15 minutes old. Browsers already
signed in keep working.

### What a browser User can do

Signed in, a User may do in the web UI exactly what their Space membership
allows, as on a device of theirs: keep the Hosts, Services and Secrets of
their Spaces, and for the Owner or an Administrator, everything they do
from a device.
As from a device, `koko server set` stays on the koko server's machine.

A Sign-in also reaches Hosts as a device of the same User does: the web
terminal opens an SSH Session, in a tab of its own, to each Managed Host
the User may reach, and its Files panel uploads and downloads files on
that Host. A Sign-in holds at most 16 SSH Sessions at once. A terminal
whose connection dropped, or whose page was reloaded, Resumes its SSH
Session within 15 minutes. A Sign-in's terminals end with the Sign-in
itself (see [Sign-ins](#sign-ins)), when its User leaves the Host's Space,
and when the Web URL changes.

A browser User who also wants `ssh`, `scp` or `koko exec`, or an agent
working for them, adds a device with **Add device** on the Account page;
its Join Code is spent by `koko join` on a computer with the CLI, which
then reaches Hosts as any device of that User does.

The Owner who uses the CLI can keep using `koko web`, which needs no
Password.
