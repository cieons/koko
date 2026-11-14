# Security policy

## Supported versions

Security fixes go into the latest release only. `koko update` and
`koko server upgrade`, or pulling the latest image for Compose, bring them in.

## Reporting a vulnerability

Report a vulnerability privately through GitHub: on this repository's
**Security** tab, choose **Report a vulnerability**. Do not open a public
issue, discussion or pull request for it.

Include what you can of:

- the versions (`koko version`, `kokod version`) and how kokod runs
  (systemd or Compose);
- the steps that show the problem, and what an attacker gains;
- logs or a proof of concept.

The report stays private between you and the maintainers until a fix is
released. The advisory then credits you, unless you ask otherwise.
