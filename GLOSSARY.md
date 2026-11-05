# koko

koko keeps the record of a fleet of rented servers and is the one door people
and agents use to reach them.

## Language

### The fleet

**Host**:
A machine koko records in one Space and can reach over SSH.
_Avoid_: VPS, server, machine, node, asset

**SSH Name**:
What `ssh` is given to reach a Host through koko: the Host's own name, alone or followed by `.koko`.
_Avoid_: alias, hostname, koko address

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
A Host that no longer exists, kept as history, left out of ordinary lists, and no longer holding its name.
_Avoid_: deleted, archived

**Service**:
A named application running on one Host, with the ports it listens on, if any.
_Avoid_: app, workload, container

**Attribute**:
A key-value pair recorded on a Host or a Service to describe, filter and group it; it never decides who can see or act on anything.
_Avoid_: tag, label, metadata

**Note**:
Free multi-line text kept on a Host or a Service for people and agents to read; unlike an Attribute, nothing filters, sorts or groups by it.
_Avoid_: description, comment, remarks

**Preset Attribute**:
An Attribute key koko ships with a fixed type, such as `expires_at` as a date.
_Avoid_: built-in field, standard tag

### koko itself

**koko server**:
One running deployment of koko, holding the record, the Secrets and the Gateway.
_Avoid_: asset server, daemon, backend

**Server Address**:
The one `host:port` a koko server writes into its Join Codes, which its devices store and connect to.
_Avoid_: advertised address, public address, endpoint, URL

**Gateway**:
The part of the koko server that ends every SSH Session and opens its own SSH connection to the Host.
_Avoid_: bastion, jump host, proxy

**koko key**:
The one SSH key pair a koko server uses to log in to every Managed Host.
_Avoid_: deploy key, master key, server key

**Root Key**:
The one key, kept outside the data directory, that seals every Secret and every private key a koko server holds.
_Avoid_: master key, encryption key, data key, KEK

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
The Space every User has to themselves, named after them, where what they record goes by default.
_Avoid_: home, private space

**Invite**:
The first Join Code of a new User, which the Owner issues and which creates that User.
_Avoid_: enroll, signup, registration, invite link

**Removed**:
A User the Owner has taken out of the koko server, kept on record so their name is never used again.
_Avoid_: deleted, deactivated, banned

### Access

**Join**:
To register a device's own key with a koko server by using a Join Code, which makes that key a Device Session.
_Avoid_: sign in, log in, pair, register, enroll

**Join Code**:
A one-time, short-lived code that lets one device join a koko server for a User; it names the koko server and pins its host key.
_Avoid_: token, pairing code, invite code, enrollment code

**Device Session**:
The key one device registered when it joined a koko server, under a name that never changes: the identity that device acts as until it is Revoked.
_Avoid_: session (alone), login, token

**Revoked**:
A Device Session that can never connect again, kept as history; its key can never join again.
_Avoid_: logged out, expired, deleted

**Local caller**:
Anything on the koko server's own machine that calls it over its Unix socket; it acts as the Owner.
_Avoid_: admin, root, console

**Device Connection**:
An SSH connection from a device to the koko server, authenticated by that device's Device Session.
_Avoid_: tunnel, session, link

**Device Mux**:
The background process on a device that holds its one Device Connection and carries every koko command and Tunnel of that device over it.
_Avoid_: daemon, agent, mux (alone), master

**SSH Session**:
One SSH connection through the Gateway to a Host.
_Avoid_: session (alone), connection

**Tunnel**:
What carries one SSH Session from the CLI to the Gateway, inside a channel on a Device Connection or over the Unix socket; it outlives a dropped Device Connection by Resuming on a new one.
_Avoid_: websocket, channel, stream

**Detached**:
A Tunnel that has lost its Device Connection, whose SSH Session the Gateway holds for a short grace period so the device can Resume it.
_Avoid_: suspended, orphaned, disconnected

**Resume**:
To carry a Detached Tunnel, and the SSH Session inside it, onto a new Device Connection of the same Device Session.
_Avoid_: reattach, reconnect, roam
