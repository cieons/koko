# koko

koko keeps the record of a fleet of rented servers and is the one door people
and agents use to reach them.

## Language

### The fleet

**Host**:
A machine koko records in one Space and can reach over SSH.
_Avoid_: VPS, server, machine, node, asset

**Host Account**:
The existing account on a Host that koko logs in as, given when the Host is recorded.
_Avoid_: login user, deploy user, koko user

**Registered**:
A Host koko has recorded but cannot log in to yet.

**Enroll**:
To make a Registered Host Managed, by pinning its host key and adding the koko key to its Host Account.
_Avoid_: onboard, adopt, provision

**Managed**:
A Host koko can log in to with its koko key.

**Relay**:
A Host the Gateway passes through to reach another Host.
_Avoid_: jump host, bastion

**Retired**:
A Host that no longer exists, kept as history.
_Avoid_: deleted, archived

**Service**:
A named application running on one Host and listening on its ports.
_Avoid_: app, workload, container

**Attribute**:
A key-value pair recorded on a Host or a Service to describe, filter and group it; it never decides who can see or act on anything.
_Avoid_: tag, label, metadata

**Preset Attribute**:
An Attribute key koko ships with a fixed type, such as `expires_at` as a date.
_Avoid_: built-in field, standard tag

### koko itself

**koko server**:
One running deployment of koko, holding the record, the Secrets and the Gateway.
_Avoid_: asset server, daemon, backend

**Gateway**:
The part of the koko server that ends every incoming SSH connection and opens its own to the Host.
_Avoid_: bastion, jump host, proxy

**koko key**:
The one SSH key pair a koko server uses to log in to every Managed Host.
_Avoid_: deploy key, master key, server key

**Secret**:
A sensitive value the koko server holds in one Space and shows only when asked to by name.
_Avoid_: credential, password

### People

**User**:
A person who belongs to a koko server, either its Owner or a Member.
_Avoid_: account, operator, admin

**Owner**:
A User who can do everything on the koko server and sees every Space.
_Avoid_: admin, root, superuser

**Member**:
A User who can see and act on every Host, Service and Secret in the Spaces they belong to, and sees nothing outside them.
_Avoid_: operator, viewer, guest

**Space**:
A container of Hosts and Secrets whose members are the only Members who can see or act on them.
_Avoid_: vault, project, group, team, folder

**Personal Space**:
The Space every User has to themselves, where what they record goes by default.
_Avoid_: home, private space

**Invite**:
A one-time link that lets a person join a koko server as a User.
_Avoid_: enroll, signup, registration

### Access

**Device Session**:
The revocable identity one device holds after signing in to a koko server.
_Avoid_: session (alone), login, token

**SSH Session**:
One SSH connection through the Gateway to a Host.
_Avoid_: session (alone), connection

**Tunnel**:
A stream from the CLI to the koko server over its one HTTPS port, carrying an SSH Session or a port forward.
_Avoid_: websocket, channel, stream
