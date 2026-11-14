# Using koko with Ansible

Ansible runs commands and configuration across many Hosts; koko gives it
their inventory and their access. `koko host ls --ansible` prints the
Managed Hosts as Ansible's dynamic inventory, and Ansible reaches each one
as `<name>.koko`, through the Gateway, which logs in as the Host Account.
koko runs no playbook itself ([ADR 0059](adr/0059-koko-gives-ansible-its-inventory-and-its-access-and-runs-no-playbooks.md)).

## What the control machine needs

The machine Ansible runs on needs:

- the `koko` CLI, joined to the koko server (`koko join`), which also
  writes the ssh_config that makes `ssh <name>.koko` work. On the koko
  server's own machine, where nothing joins, `koko ssh setup` writes it
  instead. `koko status` says whether ssh reads it.
- Ansible, with its default `ssh` connection, which reads that ssh_config.

`ssh web1.koko true` checks the path before Ansible uses it.

## The inventory

Ansible runs an inventory file, so a two-line executable wrapper calls
koko. Save it as `koko-inventory` beside the playbooks:

```sh
#!/bin/sh
exec koko host ls --ansible
```

and make it executable with `chmod +x koko-inventory`. Ansible calls it
with `--list`. The inventory holds every Host's variables in
`_meta.hostvars`, so Ansible never calls it with `--host`, and the wrapper
ignores its arguments.

`ansible.cfg` names it as the inventory:

```ini
[defaults]
inventory = ./koko-inventory
```

`ansible-inventory --graph` shows what Ansible reads.

The wrapper takes `host ls`'s filter terms, so an inventory of only some
Hosts is one line too:

```sh
#!/bin/sh
exec koko host ls region=eu --ansible
```

The inventory lists the Managed Hosts that the user of the control
machine's device can see. A Registered Host is left out, since koko
reaches only a Managed Host, and so is a Retired Host. `--ansible` takes
no `--all`, `--with-note` or `--json`.

## Hosts and variables

Each Host is named by its koko name, and has these variables:

| Variable | Value |
| --- | --- |
| `ansible_host` | `<name>.koko` |
| `koko_space` | the Host's Space |
| `koko_<key>` | each Attribute, such as `koko_role` or `koko_expires_at`; a number is a number, every other value a string |

No Attribute key is the name of a field, so `koko_space` is always the
Space. In a variable name, each character other than an ASCII letter, a
digit or `_` becomes `_`.

## Groups

| Group | Hosts |
| --- | --- |
| `all` | every Host, as in any inventory |
| `space_<space>` | the Hosts in the Space, such as `space_eu_ops` for `eu-ops` |
| `<key>_<value>` | the Hosts with that Attribute, such as `role_db` and `region_eu` |

There is a `<key>_<value>` group for each Attribute but `expires_at` and
`price`, the date and number Preset Attributes. In a group name, each
character other than an ASCII letter, a digit or `_` becomes `_`, so two
values that differ only there, such as `eu-1` and `eu_1`, share one group.

Ansible's own patterns combine the groups:

```sh
ansible 'role_db:&region_eu' -m ping
ansible-playbook site.yml --limit 'space_eu_ops:!role_db'
```

## Custom groups

A group koko does not print, such as one per role and region, comes from
Ansible's `constructed` inventory plugin, which reads koko's variables.
Put the wrapper and a `constructed` file in an inventory directory:

```
inventory/
  01-koko          # the wrapper
  02-groups.yml
```

`02-groups.yml`:

```yaml
plugin: ansible.builtin.constructed
strict: false
keyed_groups:
  - key: koko_role ~ '_' ~ koko_region
    prefix: ''
    separator: ''
groups:
  eu_databases: koko_role == 'db' and koko_region == 'eu'
```

and `inventory = ./inventory` in `ansible.cfg`. A Host with role `db` in
region `eu` then falls under `db_eu` and `eu_databases`. Ansible reads the
files in name order, so the wrapper's name sorts first. A configuration
that sets `enable_plugins` lists `script` and `auto`, which loads
`constructed`.

## User names

Playbooks must not set `ansible_user` or `remote_user` for these Hosts,
nor `-u` on the command line. The Gateway takes only the SSH user name
`koko`, which koko's ssh_config gives, and logs in on the Host as its Host
Account. To act as another user on the Host, use `become`.
