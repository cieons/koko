# Test data of koko's WebAuthn verifier

## w3c-level3.json

These are the test vectors of [Web Authentication Level 3, §16 "Test
Vectors"](https://www.w3.org/TR/webauthn-3/#sctn-test-vectors), byte for
byte: the 14 of §16.2 to §16.16 that go-webauthn carries. §16.12, an
Ed448 credential, is not among them; koko takes no Ed448 key. Each holds
a registration and the assertion made with it:

- the RP ID is `example.org`;
- the origin is `https://example.org`;
- every value is hex.

They were copied from
`protocol/specification_vectors_e2e_test.go` of
[go-webauthn](https://github.com/go-webauthn/webauthn) v0.18.2, which
quotes them with a link to each section. Where go-webauthn lists one vector
twice, with cross-origin calls allowed and refused, it is kept once.

Copyright © W3C® (MIT, ERCIM, Keio, Beihang). They are used under the [W3C
Software and Document License](https://www.w3.org/copyright/software-license-2023/).

## go-webauthn-registrations.json

These are the real-authenticator registrations in go-webauthn v0.18.2's
`protocol/attestation_test.go`, with the RP ID and origin of the options
they answered. Values are base64url without padding, as a browser's
`toJSON()` gives them. The one challenge go-webauthn wrote with padding
has had its padding removed.

| Name | Authenticator | Format |
| --- | --- | --- |
| `macos-touch-id-packed-self` | macOS Touch ID | `packed`, self attestation |
| `titan-fido-u2f-direct` | Google Titan | `fido-u2f`, direct |
| `titan-none` | Google Titan | `none` |
| `packed-virtual` | a packed authenticator | `packed` |

Copyright (c) 2025 github.com/go-webauthn/webauthn authors. They are used
under go-webauthn's BSD-3-Clause licence:

> Redistribution and use in source and binary forms, with or without
> modification, are permitted provided that the following conditions are
> met:
>
> 1. Redistributions of source code must retain the above copyright notice,
>    this list of conditions and the following disclaimer.
> 2. Redistributions in binary form must reproduce the above copyright
>    notice, this list of conditions and the following disclaimer in the
>    documentation and/or other materials provided with the distribution.
> 3. Neither the name of the copyright holder nor the names of its
>    contributors may be used to endorse or promote products derived from
>    this software without specific prior written permission.
>
> THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS
> IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
> THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
> PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR
> CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
> EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
> PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
> PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
> LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
> NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
> SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
