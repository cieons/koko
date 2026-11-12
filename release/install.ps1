# install.ps1 installs koko, the CLI, on this Windows machine, or upgrades
# it, as %USERPROFILE%\.local\bin\koko.exe for the user who runs it:
#
#   irm https://github.com/cieons/koko/releases/latest/download/install.ps1 | iex
#
# It runs in Windows PowerShell 5.1 and PowerShell 7, and needs no
# administrator. It downloads koko-windows-amd64.exe from the latest
# release, or the release KOKO_VERSION names (such as v0.6.0), checks it
# against that release's SHA256SUMS, and puts it in place by renaming a
# koko.exe that is there aside, since it may be running. When the directory
# is not in the user's PATH, the value Windows keeps under HKCU\Environment,
# it adds it there (ADR 0052). It takes no arguments and installs no koko
# server, which runs on Linux only. Nothing here prompts; it says what it
# does on the error stream and writes nothing to the output stream. It never
# calls exit, which would close the PowerShell it was pasted into: a failure
# is a terminating error.
#
# Everything sits in Install-Koko, run on the last line, so that a download
# cut short runs nothing. This file is ASCII only: Windows PowerShell 5.1
# reads a file without a byte order mark in the system's code page.

function Install-Koko {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 draws each download's progress, which slows a
    # download of this size many times over.
    $ProgressPreference = 'SilentlyContinue'

    $repo = 'https://github.com/cieons/koko'
    $asset = 'koko-windows-amd64.exe'
    # $first is the first release that has $asset.
    $first = 'v0.6.0'
    # $isTag matches a release's tag, such as v0.6.0 or v0.6.0-rc.1.
    $isTag = '\Av[0-9][0-9A-Za-z.+-]*\z'

    # Say says $text on the error stream, as install.ps1.
    function Say([string]$text) {
        [Console]::Error.WriteLine("install.ps1: $text")
    }

    # Fail stops the script, saying why.
    function Fail([string]$text) {
        throw "install.ps1: $text"
    }

    # Fetch writes the file at $url to $file. Every URL here is HTTPS.
    function Fetch([string]$url, [string]$file) {
        try {
            Invoke-WebRequest -Uri $url -OutFile $file -UseBasicParsing
        } catch {
            Fail "could not download $url"
        }
    }

    # OnPath says whether $bin is an entry of the PATH value $list, however
    # the entry spells it: with a variable such as %USERPROFILE%, in
    # quotes, in another case or with a \ at its end.
    function OnPath([string]$list) {
        foreach ($entry in $list -split ';') {
            $dir = [Environment]::ExpandEnvironmentVariables($entry).Trim('"').TrimEnd('\')
            if ($dir -eq $bin.TrimEnd('\')) {
                return $true
            }
        }
        return $false
    }

    # WithBin is the PATH value $list with $bin as its last entry.
    function WithBin([string]$list) {
        (@($list.TrimEnd(';'), $bin) | Where-Object { $_ }) -join ';'
    }

    if ($PSVersionTable.PSVersion.Major -ge 6 -and -not $IsWindows) {
        Fail 'install.ps1 installs koko on Windows, and this machine runs another system, where install.sh installs it; nothing was written'
    }
    # A 32-bit PowerShell on 64-bit Windows has the machine's own
    # architecture in PROCESSOR_ARCHITEW6432. Windows on Arm64 runs the
    # amd64 koko under emulation.
    $machine = $env:PROCESSOR_ARCHITEW6432
    if (-not $machine) {
        $machine = $env:PROCESSOR_ARCHITECTURE
    }
    if ($machine -ne 'AMD64' -and $machine -ne 'ARM64') {
        Fail "koko runs on 64-bit Windows, on x64 and Arm64, and this machine is $machine; nothing was written"
    }
    $userDir = $env:USERPROFILE
    if (-not $userDir) {
        Fail 'install.ps1 installs koko for this user in %USERPROFILE%\.local\bin, and USERPROFILE is not set; nothing was written'
    }
    if (-not (Test-Path -LiteralPath $userDir -PathType Container)) {
        Fail "install.ps1 installs koko for this user in %USERPROFILE%\.local\bin, and USERPROFILE=$userDir is not a directory; nothing was written"
    }
    $bin = Join-Path $userDir '.local\bin'
    $target = Join-Path $bin 'koko.exe'

    # Windows PowerShell 5.1 may be set to offer only TLS older than 1.2,
    # which GitHub refuses; the system's default, 0, already offers it. The
    # setting is the session's, so it is put back at the end.
    $tls = [Net.ServicePointManager]::SecurityProtocol
    $olderTls = $PSVersionTable.PSVersion.Major -lt 6 -and [int]$tls -ne 0 -and
        -not ($tls -band [Net.SecurityProtocolType]::Tls12)
    $tmp = $null
    try {
        if ($olderTls) {
            [Net.ServicePointManager]::SecurityProtocol = $tls -bor [Net.SecurityProtocolType]::Tls12
        }

        if ($env:KOKO_VERSION) {
            $tag = 'v' + ($env:KOKO_VERSION -creplace '\Av', '')
            if ($tag -cnotmatch $isTag) {
                Fail "KOKO_VERSION=$($env:KOKO_VERSION) is not a koko version, such as $first"
            }
        } else {
            # GitHub redirects /releases/latest to the latest release's tag
            # page, through the repository's new path if it moved. It is
            # read once, so koko and SHA256SUMS come from one release. The
            # page's address is where each PowerShell keeps it.
            try {
                $answer = (Invoke-WebRequest -Uri "$repo/releases/latest" -Method Head -UseBasicParsing).BaseResponse
            } catch {
                Fail "could not find koko's latest release at $repo/releases/latest"
            }
            if ($PSVersionTable.PSVersion.Major -lt 6) {
                $page = $answer.ResponseUri.AbsoluteUri
            } else {
                $page = $answer.RequestMessage.RequestUri.AbsoluteUri
            }
            $tag = ''
            if ($page -cmatch '\Ahttps://.*/releases/tag/([^/]+)\z') {
                $tag = $Matches[1]
            }
            if ($tag -cnotmatch $isTag) {
                Fail "found no koko release at $repo/releases/latest"
            }
        }

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('koko-install-' + [guid]::NewGuid().ToString('N'))
        try {
            New-Item -ItemType Directory -Path $tmp | Out-Null
        } catch {
            Fail 'could not make a temporary directory'
        }

        Say "downloading koko $tag for windows/amd64"
        $sums = Join-Path $tmp 'SHA256SUMS'
        Fetch "$repo/releases/download/$tag/SHA256SUMS" $sums
        $want = ''
        foreach ($line in Get-Content -LiteralPath $sums) {
            $fields = $line -split '\s+', 2
            if ($fields.Count -eq 2 -and ($fields[1] -ceq $asset -or $fields[1] -ceq "*$asset")) {
                $want = $fields[0]
                break
            }
        }
        if (-not $want) {
            # A release from before the Windows CLI lists no $asset.
            $core = '\Av(\d{1,9})\.(\d{1,9})\.(\d{1,9})'
            if ($tag -cmatch $core) {
                $version = [version]($Matches[1..3] -join '.')
                if ($first -cmatch $core -and $version -lt [version]($Matches[1..3] -join '.')) {
                    Fail "koko $tag has no Windows CLI; the first release with one is $first"
                }
            }
            Fail "the SHA256SUMS of koko $tag lists no $asset, so it was not installed"
        }
        $file = Join-Path $tmp $asset
        Fetch "$repo/releases/download/$tag/$asset" $file
        try {
            $got = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash
        } catch {
            Fail "could not compute the SHA-256 of $asset, so it was not installed"
        }
        if ($got -ne $want) {
            Fail "$asset does not match the SHA256SUMS of koko ${tag}: the download is damaged or was swapped, so it was not installed"
        }

        # The CLI is written beside koko.exe and put in place by two
        # renames, as koko update does it (ADR 0052): Windows lets no one
        # replace or delete a running program's file, but lets it be renamed
        # within its directory, where a koko that runs from it, such as a
        # Device Mux, runs on. Between the renames there is no koko.exe.
        # The new file has one name, so a run cut short leaves at most one,
        # which the next run replaces.
        $new = Join-Path $bin '.koko.new.exe'
        try {
            New-Item -ItemType Directory -Force -Path $bin | Out-Null
            Copy-Item -LiteralPath $file -Destination $new -Force
        } catch {
            Fail "could not write ${new}: $($_.Exception.Message)"
        }
        $old = $null
        if (Test-Path -LiteralPath $target) {
            $old = Join-Path $bin ('.koko.old-' + [guid]::NewGuid().ToString('N') + '.exe')
            try {
                Move-Item -LiteralPath $target -Destination $old
            } catch {
                Fail "could not rename ${target} aside: $($_.Exception.Message); nothing was changed"
            }
        }
        try {
            Move-Item -LiteralPath $new -Destination $target
        } catch {
            $why = $_.Exception.Message
            if ($old) {
                try {
                    Move-Item -LiteralPath $old -Destination $target
                } catch {
                    Fail "could not write ${target}: $why; the koko that was there is now $old and could not be renamed back: rename it to koko.exe"
                }
            }
            Fail "could not write ${target}: $why; nothing was changed"
        }
        # Every koko renamed aside, by this run, an earlier one or koko
        # update, is deleted once no process runs it. One that still runs
        # cannot be: it is hidden and left for the next run.
        foreach ($left in Get-ChildItem -LiteralPath $bin -Force -Filter '.koko.old-*.exe') {
            try {
                Remove-Item -LiteralPath $left.FullName -Force
            } catch {
                try {
                    $left.Attributes = $left.Attributes -bor [IO.FileAttributes]::Hidden
                } catch {
                    # It stays as it is, in view.
                }
            }
        }
        Say "installed koko $tag at $target"

        # The user's PATH is read as the registry holds it, its variables
        # unexpanded, and written back with the type it had. The machine's
        # PATH is never touched.
        $key = Get-Item -LiteralPath 'HKCU:\Environment'
        $path = $key.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
        if ($null -ne $path) {
            $kind = $key.GetValueKind('Path')
        }
        $key.Close()
        if ($kind -ne 'String' -and $kind -ne 'ExpandString') {
            Say "this user's PATH is not text in the registry, so it was left as it is: add $bin to it by hand"
        } elseif (-not (OnPath "$path")) {
            try {
                Set-ItemProperty -LiteralPath 'HKCU:\Environment' -Name 'Path' -Value (WithBin "$path") -Type $kind
            } catch {
                Fail "koko is installed, but $bin could not be added to this user's PATH: $($_.Exception.Message)"
            }
            # Programs that are running, Explorer among them, read the
            # environment again when told that it changed
            # (WM_SETTINGCHANGE), so a terminal opened from now on finds
            # koko.
            try {
                $member = '[DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);'
                $user32 = Add-Type -Namespace Koko -Name User32 -MemberDefinition $member -PassThru
                [UIntPtr]$result = [UIntPtr]::Zero
                # To every top-level window (0xffff), WM_SETTINGCHANGE
                # (0x1a), giving up on a window that hangs (2) and after 5 s.
                [void]$user32::SendMessageTimeout([IntPtr]0xffff, 0x1a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
            } catch {
                Say 'could not tell running programs that PATH changed: sign out and in again for them to find koko'
            }
            if (-not (OnPath "$($env:Path)")) {
                $env:Path = WithBin "$($env:Path)"
            }
            Say "added $bin to this user's PATH; a terminal that is already open must be reopened to find koko"
        }

        # The koko a command line finds first, when it is not the new one.
        # install.ps1 removes nothing.
        $found = Get-Command -Name koko -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($found -and $found.Path -ne $target) {
            Say "koko on PATH is $($found.Path), not the new $target"
        }
    } finally {
        if ($tmp) {
            Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
        }
        if ($olderTls) {
            [Net.ServicePointManager]::SecurityProtocol = $tls
        }
    }
}

Install-Koko
