# The command path carries the risk, and the command line is shell-safe

Agents drive koko through a shell, and their harnesses allow or ask by
command prefix (ADR 0006), so the CLI's grammar is part of its safety. Every
command is `koko <noun> <verb>`, and its path alone says what it can do:
read (`ls`, `show`, `status`), change the record reversibly (`add`, `set`,
`rename`, `move`), or do something irreversible, reach a machine or print a
credential (`retire`, `rm`, `revoke`, `user remove`, `host enroll`, `exec`,
`secret show`, `device add`, `user invite`). No flag raises a command's
risk, and koko refuses flags written before the command path, so `koko
--json secret show` cannot slip past a rule written for `koko secret ls`.
Filters use only characters a shell leaves alone, `expires_at.le=+30d`
rather than `expires_at<=+30d`, because an unquoted `>` silently writes the
list into a file and filters on the key alone.

## Considered Options

- **Comparison operators, quoted** (`'price<10'`): familiar, but a forgotten
  quote truncates a file and gives a wrong answer with no error.
- **Both spellings**: twice the syntax to document and test, with the unsafe
  one still accepted.
- **Flags anywhere on the line**, as CLI libraries allow by default: a
  prefix rule would no longer see the real command.

## Consequences

- koko prompts only on a terminal, and only for a secret input it was not
  given (a Secret value, an Enroll password, a key's passphrase), with echo
  off. Without a terminal it fails at once and names the flag or stdin form.
  Nothing asks "are you sure?": the risk is in the command's name. The one
  confirmation, revoking the Owner's own last Device Session, is a flag.
- Errors go to stderr, and with `--json` so does the error object, leaving
  stdout empty. A command exits 0 on success, 1 when the operation failed,
  and 2 when the CLI refused its command line before calling the koko
  server. `koko exec` exits with the remote command's status, and 255 when
  koko itself fails, as `ssh` does. These statuses are part of the CLI's
  contract (ADR 0016).
- Qualified names use `/`, which no name may hold: `<space>/<secret>`,
  `<host>/<service>`, `<user>/<device>`. A Secret is always named with its
  Space, so `koko secret show` never guesses which one.
- Fields and Attributes are both written `key=value`, kept apart by the
  reserved keys. `--unset key` clears one, and an empty value is refused, so
  an unset shell variable never clears a value by accident.
