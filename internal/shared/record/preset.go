package record

import "slices"

// Preset is a Preset Attribute: a key koko ships with a fixed type, the same
// on a Host and on a Service (ADR 0010).
type Preset struct {
	Key  string
	Type Type
}

// presets are the P0 Presets (P0 spec: the record), in the order the spec
// gives them, which koko attr ls keeps.
var presets = []Preset{
	{Key: "provider", Type: Text},
	{Key: "provider_account", Type: Text},
	{Key: "region", Type: Text},
	{Key: "price", Type: Type{kind: KindNumber, nonNegative: true}},
	{Key: "currency", Type: Type{kind: KindEnum, values: iso4217, about: "the ISO 4217 codes, such as EUR, USD and CNY"}},
	{Key: "billing_cycle", Type: Enum("monthly", "quarterly", "semiannual", "yearly", "biennial", "triennial")},
	{Key: "expires_at", Type: Date},
	{Key: "auto_renew", Type: Enum("no", "yes")},
}

// Presets returns the Preset Attributes, in their table's order.
func Presets() []Preset {
	return slices.Clone(presets)
}

// TypeOf returns the type of the Attribute key's values: its Preset's, or
// Text for every other key.
func TypeOf(key string) Type {
	if i := slices.IndexFunc(presets, func(p Preset) bool { return p.Key == key }); i >= 0 {
		return presets[i].Type
	}
	return Text
}

// IsPreset tells whether key is a Preset Attribute's.
func IsPreset(key string) bool {
	return slices.ContainsFunc(presets, func(p Preset) bool { return p.Key == key })
}

// iso4217 are the alphabetic codes of ISO 4217's List One as SIX, its
// maintenance agency, published it on 2026-09-17, sorted, which is the
// order currency's values sort in. A code withdrawn later stays, so a value
// once recorded keeps parsing; a code added later is added here.
var iso4217 = []string{
	"AED", "AFN", "ALL", "AMD", "AOA", "ARS", "AUD", "AWG", "AZN", "BAM", "BBD", "BDT", "BHD", "BIF", "BMD", "BND",
	"BOB", "BOV", "BRL", "BSD", "BTN", "BWP", "BYN", "BZD", "CAD", "CDF", "CHE", "CHF", "CHW", "CLF", "CLP", "CNY",
	"COP", "COU", "CRC", "CUP", "CVE", "CZK", "DJF", "DKK", "DOP", "DZD", "EGP", "ERN", "ETB", "EUR", "FJD", "FKP",
	"GBP", "GEL", "GHS", "GIP", "GMD", "GNF", "GTQ", "GYD", "HKD", "HNL", "HTG", "HUF", "IDR", "ILS", "INR", "IQD",
	"IRR", "ISK", "JMD", "JOD", "JPY", "KES", "KGS", "KHR", "KMF", "KPW", "KRW", "KWD", "KYD", "KZT", "LAK", "LBP",
	"LKR", "LRD", "LSL", "LYD", "MAD", "MDL", "MGA", "MKD", "MMK", "MNT", "MOP", "MRU", "MUR", "MVR", "MWK", "MXN",
	"MXV", "MYR", "MZN", "NAD", "NGN", "NIO", "NOK", "NPR", "NZD", "OMR", "PAB", "PEN", "PGK", "PHP", "PKR", "PLN",
	"PYG", "QAR", "RON", "RSD", "RUB", "RWF", "SAR", "SBD", "SCR", "SDG", "SEK", "SGD", "SHP", "SLE", "SOS", "SRD",
	"SSP", "STN", "SVC", "SYP", "SZL", "THB", "TJS", "TMT", "TND", "TOP", "TRY", "TTD", "TWD", "TZS", "UAH", "UGX",
	"USD", "USN", "UYI", "UYU", "UYW", "UZS", "VED", "VES", "VND", "VUV", "WST", "XAD", "XAF", "XAG", "XAU", "XBA",
	"XBB", "XBC", "XBD", "XCD", "XCG", "XDR", "XOF", "XPD", "XPF", "XPT", "XSU", "XTS", "XUA", "XXX", "YER", "ZAR",
	"ZMW", "ZWG",
}
