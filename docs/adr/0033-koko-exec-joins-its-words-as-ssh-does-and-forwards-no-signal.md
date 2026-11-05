# koko exec joins its words as ssh does and forwards no signal

`koko exec <host> <command...>` takes every word after the Host as the
command's, flags included, and joins them with single spaces into one
command line, as `ssh host <command...>` does; the Host Account's shell
reads that line. So `koko exec web1 'cd /srv && ls'` and
`koko exec web1 ls -l /srv` work as they would after `ssh web1`, and a word
that must stay one word through the Host's shell is quoted for that shell,
as with ssh. It forwards no signal: Ctrl-C, or the SIGTERM an agent's
harness sends at a timeout, ends the SSH Session and exits 255, as it ends
`ssh` without a pty.

## Considered Options

- **Quote each word for a POSIX shell**, so the command's argv arrives
  exactly as typed: `koko exec web1 echo 'a  b'` keeps both spaces. But the
  commonest form, one string holding a whole command line with `&&`, `|`
  or `>`, would reach the Host as a single word naming no program, and an
  agent's habits from ssh would fail. A Host Account's shell need not be
  POSIX either.
- **Quote only when there are several words**: argv-exact for some command
  lines and shell-read for others, a rule nobody guesses.
- **Forward SIGINT, SIGTERM and SIGHUP as SSH `signal` requests**: a
  command a harness times out would stop on the Host too. It needs signal
  handling per OS, a way to test it that no test hook gives, and an sshd
  that honours `signal` requests (OpenSSH 7.9 or later); an sshd that
  ignores them would leave Ctrl-C doing nothing.

## Consequences

- An agent writes the command for koko exec as it would for ssh, and quotes
  for the Host's shell where it would for ssh.
- A command an interruption leaves running on the Host stops only when it
  next reads its closed stdin or writes to its closed stdout or stderr, as
  after ssh without a pty.
- Forwarding signals can come later without breaking a script: a command
  that today ends with koko exec would then end a little sooner.
- `koko exec` takes no flag between `exec` and the Host either, so a later
  option goes there without taking a word from any command.
