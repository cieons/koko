package passkey_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/passkey"
	"github.com/cieons/koko/internal/shared/api"
)

// Two answers of one passkey, each verified against the counter stored
// before either is written, cannot both sign in: the write of the second
// finds the counter moved and refuses it, so the counter never goes back.
func TestTwoAnswersVerifiedAtOnceCannotBothSignIn(t *testing.T) {
	h := harness.Start(t, harness.WebListen("127.0.0.1:0"))
	h.Browser() // sets the Web URL
	h.Koko("user", "invite", "bob").WantSuccess(t)
	auth := harness.NewAuthenticator(harness.WebURL)
	if status, body := h.SignIn("bob").AddPasskey(auth, "", "Laptop"); status != http.StatusOK {
		t.Fatal(status, body)
	}

	// Both answers wait once verified until the other has verified too.
	var arrived sync.WaitGroup
	arrived.Add(2)
	passkey.HoldVerified(t.Cleanup, func() {
		arrived.Done()
		done := make(chan struct{})
		go func() { arrived.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	type answered struct {
		status int
		body   string
	}
	var (
		mu      sync.Mutex
		answers []answered
		sent    sync.WaitGroup
	)
	for range 2 {
		b := h.Browser()
		c := b.BeginPasskeySignIn()
		answer, err := auth.Get(c.Options)
		if err != nil {
			t.Fatal(err)
		}
		sent.Go(func() {
			status, body := b.Answer(api.AuthPasskeySignInPath, c, answer, "")
			mu.Lock()
			defer mu.Unlock()
			answers = append(answers, answered{status, body})
		})
	}
	sent.Wait()
	signedIn := 0
	for _, a := range answers {
		if a.status == http.StatusOK {
			signedIn++
		}
	}
	if signedIn != 1 {
		t.Errorf("of two answers verified at once, %d signed in, want 1: %+v", signedIn, answers)
	}
}
