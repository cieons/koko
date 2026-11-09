# The web UI is also served over plain HTTP to the Owner's reverse proxy

`kokod` serves the web UI and its API on a second listener, plain HTTP/1.1,
meant only for a reverse proxy the Owner runs, the Owner's choice of
2026-11-09. The listener is off until `kokod run --web-listen <host:port>`
(`KOKO_WEB_LISTEN`) turns it on, and the docs bind it to loopback, such as
`127.0.0.1:7823`. The proxy, such as Caddy, holds the domain and its TLS
certificate. The address it serves is the Web URL, an `https://` origin the
Owner stores with `koko server set web_url=…`. The koko server checks every
request's Host and every `POST`'s Origin against it, and prints it in every
Password Link. This lets people who will never install the CLI, phones
included, use the web UI.

This supersedes ADR 0012's "the web UI is not exposed on the network … being
able to run it is the sign-in" and its consequence that phones have no
access, and refines ADR 0006's "reached only through `koko web`". ADR 0012's
SSH end to end and its reason for no TLS in koko stay: the proxy holds the
certificate, so a certificate is never an install step of koko. `koko web`
and its cookie (ADR 0022) are unchanged beside the new way.

## Considered Options

- **koko doing TLS itself, with ACME**: no proxy to run, but a certificate
  and a domain become an install step, which ADR 0012 rejected.
- **The listener on the SSH port, told apart by the client's first bytes**:
  one port, but plain HTTP would reach the internet unencrypted.
- **The listener on by default**: one step fewer, but an upgrade would open a
  port nobody asked for.
- **A configured list of trusted proxies**: one more setting, for a listener
  only a proxy reaches. The client's address is the last `X-Forwarded-For`
  entry, the one the proxy appended.
- **`X-Forwarded-Host` instead of a stored Web URL**: nothing to configure,
  but the Host and Origin checks would trust the header they check.

## Consequences

- An Owner who exposes the listener directly serves the web UI without TLS
  and lets a client choose the address the per-address sign-in limit counts.
  The docs say to keep it behind the proxy, and `kokod run` warns at start
  when it listens on a non-loopback address outside a container.
- A proxy must pass the Host header through. Caddy does by default; nginx
  needs `proxy_set_header Host $host`. A request for another Host gets a
  `421` page naming the Web URL.
- The Web URL must be `https://`, since the sign-in cookie is `Secure`.
  Serving under a path of it is not supported.
- Changing the Web URL signs every browser out, since their cookies belong
  to the old host, and voids the Password Links printed for it.
- The listener is where a browser terminal and the SSH-over-WSS fallback can
  later come, behind the same proxy; neither is part of this decision.
