# koko gives Ansible its inventory and its access, and runs no playbooks

koko's users run commands and configuration across many Hosts with Ansible.
koko already gives Ansible its access: a Managed Host is `<name>.koko`
through the ssh_config koko writes, and the Gateway logs in as the Host
Account. koko now gives Ansible its inventory too: `koko host ls --ansible`
prints Ansible's dynamic inventory JSON, the `--list` form with
`_meta.hostvars`, of the Managed Hosts `koko host ls` lists with the same
filters. koko itself runs no playbook and no command across many Hosts;
that stays Ansible's job, and a custom group is the job of Ansible's
`constructed` inventory plugin, not koko's.

The inventory's form is part of the CLI's contract, as `--json` is:

- Each Host is named by its koko name, with `ansible_host` set to
  `<name>.koko`. It has no `ansible_user`, port or password, since the
  Gateway takes only the SSH user name `koko`.
- Its variables are `koko_space` and `koko_<key>` for each Attribute, its
  value typed as in `--json`. No Attribute key is the name of a field,
  `space` included, so `koko_<key>` never stands for `koko_space`; a
  variable for a field koko adds later is safe in the same way.
- Its groups are `space_<space>`, and `<key>_<value>` for each Attribute but
  the date and number Presets (`expires_at`, `price`), whose values make no
  useful group. Every group is a child of `all`.
- In a variable or a group name, each character other than an ASCII letter,
  a digit or `_` becomes `_`, as Ansible's own
  `TRANSFORM_INVALID_GROUP_CHARS` does when set to `always`. Two values
  that differ only there, such as `eu-1` and `eu_1`, share one group.

## Considered Options

- **A command that runs one command on many Hosts, such as a `koko exec`
  over a filter:** it would grow into a second, weaker Ansible, with its
  own rules for parallelism, failures and output, and Ansible already does
  all of it over the access koko gives.
- **Leaving the inventory to each user's script over `koko host ls
  --json`:** each such script breaks silently when that JSON changes, and
  none is tested. The inventory koko prints is tested with koko.
- **An Ansible inventory plugin in Python, or a collection on Galaxy:**
  richer options in Ansible's own syntax, but a second language and a
  second release to keep. A two-line wrapper over the CLI does the same,
  and `constructed` covers the custom groups.
- **Registered Hosts in the inventory, marked as unreachable:** koko
  reaches only a Managed Host, so a play would only fail on them.
