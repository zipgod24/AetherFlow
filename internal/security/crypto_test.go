package security

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestSealerRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	s, err := NewSealer(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	pt := "sk-correct-horse-battery-staple"
	ct, err := s.Seal(pt)
	if err != nil {
		t.Fatal(err)
	}
	if ct == pt {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := s.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != pt {
		t.Fatalf("got %q want %q", got, pt)
	}
}

func TestSealerRejectsBadKey(t *testing.T) {
	if _, err := NewSealer(""); err == nil {
		t.Fatal("empty key accepted")
	}
	short := base64.StdEncoding.EncodeToString([]byte("toosmall"))
	if _, err := NewSealer(short); err == nil {
		t.Fatal("short key accepted")
	}
}
