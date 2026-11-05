# A number Attribute is a decimal a JavaScript number holds

A number Attribute, such as `price`, is an exact decimal of at most 15
digits, at most 6 of them after the point, written in plain decimal (`9.5`,
`120`, no exponent), and a number type may set a least value, as `price`'s
0. koko stores it in one form, with no leading zeros, no
trailing zeros after the point and no `-0`, so `price=9.90` is stored and
shown as `9.9`, and `1.10` equals `1.1`. JSON carries it as a number written
exactly as stored, and no part of koko's path (the API, the CLI, `koko host
add`'s reader) decodes it through binary floating point. Within these limits
every decimal is exactly one JavaScript number, which prints back as the same
decimal with no exponent, so the web UI's generated type for an Attribute
value is `string | number`, and its plain `JSON.parse` changes nothing.

## Considered Options

- **Any decimal, kept as written**: no limits and `9.90` stays `9.90`, but
  the web UI's `JSON.parse` would round a long decimal and drop trailing
  zeros, so the same Host would show one price in the CLI and another in the
  browser, and the web UI would need a JSON parser of its own.
- **A JSON string holding the decimal**: exact everywhere, but against the
  spec's JSON number, and every script would compare prices as strings,
  where `10` sorts before `9.5`.
- **Integer minor units, such as cents**: exact, but the unit then depends
  on the currency (JPY has none, BHD has three), which the record does not
  enforce, and an agent reading a welcome email would have to convert.

## Consequences

- `koko host set web1 price=9.90` shows `9.9` afterwards, in every client.
- A sixteenth digit, a seventh fractional digit or an exponent is refused,
  never rounded: a price of 0.0000001 or a number past 999,999,999,999,999
  cannot be recorded.
- Comparing two numbers compares their stored text by value, so neither the
  koko server nor the CLI converts one to a float to filter or sort.
- Raising the limits later keeps every stored value valid, but the web UI
  would then need a decimal type of its own.
