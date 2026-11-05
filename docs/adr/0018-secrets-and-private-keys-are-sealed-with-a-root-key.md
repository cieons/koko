# Secrets and private keys are sealed with a Root Key outside the data directory

The koko server seals every Secret value and every private key it holds, the
koko key and its own SSH host key, with AES-256-GCM under one Root Key: a
256-bit key in a file of its own, outside the data directory. A copy of the
database or of the data directory then reveals no Secret and no private key,
and the koko server still starts unattended after every reboot. The rest of
the record stays plain: Hosts, Services, Attributes, Notes and Secret names.
Encryption at rest does not guard against a shell on the koko server's machine,
which acts as the Owner already (ADR 0008), or against the hosting provider,
which controls the machine.

## Considered Options

- **The key file beside the database**: one thing to copy and one to lose,
  but it guards only a lone copy of the database file. A copy without its
  `-wal` file loses recent writes, so people copy the whole directory.
- **A passphrase at each start**: it would also guard a disk snapshot, but
  after every reboot, the provider's maintenance reboots included, the
  Gateway stays shut until the Owner unlocks it.
- **The OS keyring or an external KMS**: a headless Linux server has no
  keyring, and a KMS ties a self-hosted tool to a cloud account.
- **Encrypting the whole database file**: only `ncruces/go-sqlite3` offers
  it, deterministic and without tamper detection, so it would reopen the
  choice of SQLite driver to hide a record that holds no credentials.
- **XChaCha20-Poly1305**: its longer nonce buys nothing at koko's scale, far
  below 2^32 seals per key, and AES-GCM is in the standard library.

## Consequences

- Each ciphertext carries a format version and the Root Key's id, and is
  bound to its row's immutable id as additional data. It cannot be moved to
  another row, and moving a Secret between Spaces does not reseal it.
- There are two things to lose, not one. The koko server refuses to start
  when the Root Key is missing or its id does not match the ciphertexts,
  naming the file and the id it expects, and it never makes a new key over
  sealed data. Without a copy of the Root Key, the only way on is to move the
  data directory aside and initialise again, recording and Enrolling every
  Host anew.
- P0 has no backup, so initialisation tells the Owner where the Root Key is
  and to keep a copy off the machine. It never prints the key, which keeps it
  out of an agent's context. A copy of the data directory taken while the
  koko server is stopped, plus the Root Key, restores the koko server on
  another machine, and the copy alone reveals nothing wherever it is kept.
- Credentials belong in Secrets only: a Note or an Attribute is stored plain.
- The Root Key is made at initialisation, before the koko key and the host
  key it seals.
- Rotating the Root Key is P1, with backup and export. The key id in every
  ciphertext leaves room for it.
