package harness

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"

	"github.com/cieons/koko/internal/shared/api"
)

// A Ceremony is a passkey ceremony a Browser began on the web listener: the
// flow its answer names, and the options to hand an Authenticator.
type Ceremony struct {
	Flow    string
	Options []byte
}

// begin posts req to the begin endpoint path and returns the ceremony it
// began, failing the test unless the koko server answers one.
func (b *Browser) begin(path string, req any) Ceremony {
	b.t.Helper()
	status, body := b.Post(path, req)
	var resp struct {
		Flow    string         `json:"flow"`
		Options jsontext.Value `json:"options"`
	}
	if err := json.Unmarshal([]byte(body), &resp); status != http.StatusOK || err != nil || resp.Flow == "" {
		b.t.Fatalf("%s answered %d %s, want a flow", path, status, body)
	}
	return Ceremony{Flow: resp.Flow, Options: resp.Options}
}

// BeginPasskeySignIn begins signing in with a passkey from b.
func (b *Browser) BeginPasskeySignIn() Ceremony {
	b.t.Helper()
	return b.begin(api.AuthPasskeySignInBeginPath, api.AuthPasskeySignInBeginRequest{})
}

// BeginPasskeyAdd begins adding a passkey from b: through the Sign-in Link
// link, or from b's Sign-in when link is "".
func (b *Browser) BeginPasskeyAdd(link string) Ceremony {
	b.t.Helper()
	return b.begin(api.AuthPasskeyAddBeginPath, api.AuthPasskeyAddBeginRequest{Link: LinkSecret(link)})
}

// Answer posts the Authenticator's answer to the ceremony c, as a page
// posts the browser's toJSON(), to the answer's endpoint path, with name
// when it is not "", and returns the status and body of the koko server's
// answer, keeping the cookie it sets.
func (b *Browser) Answer(path string, c Ceremony, answer []byte, name string) (int, string) {
	b.t.Helper()
	req := map[string]any{"flow": c.Flow, "credential": jsontext.Value(answer)}
	if name != "" {
		req["name"] = name
	}
	return b.Post(path, req)
}

// AddPasskey makes a passkey with auth from b, through the Sign-in Link
// link, or from b's Sign-in when link is "", named name when it is not "",
// and returns the status and body of /auth/passkey/add's answer. It fails
// the test when auth refuses the options, as a browser would.
func (b *Browser) AddPasskey(auth *Authenticator, link, name string) (int, string) {
	b.t.Helper()
	c := b.BeginPasskeyAdd(link)
	answer, err := auth.Create(c.Options)
	if err != nil {
		b.t.Fatalf("the authenticator refused the creation options: %v", err)
	}
	return b.Answer(api.AuthPasskeyAddPath, c, answer, name)
}

// PasskeySignIn signs b in with a passkey auth holds, and returns the status
// and body of /auth/passkey/signin's answer. It fails the test when auth
// holds no passkey for the options, as a browser would.
func (b *Browser) PasskeySignIn(auth *Authenticator) (int, string) {
	b.t.Helper()
	c := b.BeginPasskeySignIn()
	answer, err := auth.Get(c.Options)
	if err != nil {
		b.t.Fatalf("the authenticator refused the request options: %v", err)
	}
	return b.Answer(api.AuthPasskeySignInPath, c, answer, "")
}
