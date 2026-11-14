package enroll

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
)

// unlock decrypts a key only in one of the enroller's slots, so no more
// than maxDecrypts KDFs run at once: with every slot held it waits, here
// until its context ends, and once a slot is free it decrypts and gives the
// slot back.
func TestUnlockDecryptsOnlyInAFreeSlot(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("opens it")
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", passphrase)
	if err != nil {
		t.Fatal(err)
	}
	locked := input{refusedAs: "the private key", locked: &lockedKey{key: pem.EncodeToMemory(block), passphrase: passphrase}}
	e := &enroller{decrypts: make(chan struct{}, maxDecrypts)}
	for range maxDecrypts {
		e.decrypts <- struct{}{}
	}

	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.unlock(ended, locked); !errors.Is(err, context.Canceled) {
		t.Fatalf("unlock with every slot held: got %v, want it to wait for a slot until its context ended", err)
	}

	<-e.decrypts
	in, err := e.unlock(t.Context(), locked)
	if err != nil || in.locked != nil || reflect.ValueOf(in.login).IsZero() {
		t.Fatalf("unlock with a slot free: got %+v, %v, want the key decrypted", in, err)
	}
	if held := len(e.decrypts); held != maxDecrypts-1 {
		t.Errorf("after unlock %d slots are held, want %d: it keeps the slot it took", held, maxDecrypts-1)
	}
}
