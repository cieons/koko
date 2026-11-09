package signin

import "strings"

// A browser or a system, as a client label names it, found in a User-Agent
// by the first of its marks that it holds.
type uaName struct {
	name  string
	marks []string
}

// browsers are the common browsers, most specific first: Edge and Opera
// carry Chrome's mark too, and Chrome carries Safari's.
var browsers = []uaName{
	{"Edge", []string{"Edg/", "EdgiOS/", "EdgA/"}},
	{"Opera", []string{"OPR/", "OPiOS/"}},
	{"Firefox", []string{"Firefox/", "FxiOS/"}},
	{"Chrome", []string{"Chrome/", "CriOS/"}},
	{"Safari", []string{"Safari/"}},
}

// systems are the common systems, most specific first: an iPhone's
// User-Agent says "like Mac OS X", and Android's says Linux.
var systems = []uaName{
	{"iPhone", []string{"iPhone"}},
	{"iPad", []string{"iPad"}},
	{"Android", []string{"Android"}},
	{"Windows", []string{"Windows"}},
	{"ChromeOS", []string{"CrOS"}},
	{"macOS", []string{"Macintosh", "Mac OS X"}},
	{"Linux", []string{"Linux"}},
}

// clientLabel is the short label of the browser a User-Agent names, such as
// "Safari on iPhone" or "Firefox", or "Browser" when it names none of the
// common ones.
func clientLabel(userAgent string) string {
	browser, system := find(browsers, userAgent), find(systems, userAgent)
	switch {
	case browser == "":
		return "Browser"
	case system == "":
		return browser
	}
	return browser + " on " + system
}

func find(names []uaName, userAgent string) string {
	for _, n := range names {
		for _, m := range n.marks {
			if strings.Contains(userAgent, m) {
				return n.name
			}
		}
	}
	return ""
}
