package credentials

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func newFuzzManager() *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	return New(logger, "http://localhost:4533")
}

// FuzzEncryptRoundTrip checks that a stored credential decrypts to exactly what was stored,
// whatever bytes it holds. Passwords come straight from requests, so they need not be UTF-8.
func FuzzEncryptRoundTrip(f *testing.F) {
	f.Add("secret", "", "")
	f.Add("", "26719a1196d2a940705a59634eb18eab", "c19b2d")
	f.Add("secret\xff", "", "")
	f.Add("", "\xfe", "\xff")
	f.Add("pass\x00word", "tok", "salt")

	cm := newFuzzManager()
	f.Fuzz(func(t *testing.T, password, token, salt string) {
		cred := Credential{Password: password, Token: token, Salt: salt}
		encrypted, err := cm.encrypt(cred)
		if err != nil {
			t.Fatalf("encrypt(%q): %v", cred, err)
		}
		decrypted, err := cm.decrypt(encrypted)
		if err != nil {
			t.Fatalf("decrypt(encrypt(%q)): %v", cred, err)
		}
		if decrypted != cred {
			t.Fatalf("round trip changed the credential: stored %q, got back %q", cred, decrypted)
		}
	})
}

// FuzzSecretKeyDistinct checks that two different username/credential pairs never share a
// secret key. A shared key would let one secret pass as verified for the other.
func FuzzSecretKeyDistinct(f *testing.F) {
	f.Add("alice", "secret\xff", "", "", "alice", "secret\xfe", "", "")
	f.Add("alice", "pw", "", "", "alice", "", "pw", "")
	f.Add("alice", "", "t", "s", "alice", "", "ts", "")
	f.Add("a\x00b", "c", "", "", "a", "b\x00c", "", "")

	cm := newFuzzManager()
	f.Fuzz(func(t *testing.T, user1, password1, token1, salt1, user2, password2, token2, salt2 string) {
		cred1 := Credential{Password: password1, Token: token1, Salt: salt1}
		cred2 := Credential{Password: password2, Token: token2, Salt: salt2}
		if user1 == user2 && cred1 == cred2 {
			if cm.secretKey(user1, cred1) != cm.secretKey(user2, cred2) {
				t.Fatalf("secretKey isn't deterministic for %q %q", user1, cred1)
			}
			return
		}
		if cm.secretKey(user1, cred1) == cm.secretKey(user2, cred2) {
			t.Fatalf("different secrets share a key: %q %q and %q %q", user1, cred1, user2, cred2)
		}
	})
}

// FuzzUnmarshalCredential checks that decoding arbitrary bytes never panics, and that
// whatever decodes encodes back to the same bytes, so each credential has one encoding.
func FuzzUnmarshalCredential(f *testing.F) {
	f.Add(Credential{Password: "secret"}.marshal())
	f.Add(Credential{Token: "abc", Salt: "def"}.marshal())
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01})
	f.Add([]byte{5, 'a'})

	f.Fuzz(func(t *testing.T, data []byte) {
		cred, err := unmarshalCredential(data)
		if err != nil {
			return
		}
		if got := cred.marshal(); string(got) != string(data) {
			t.Fatalf("%x decoded to %q, which encodes as %x", data, cred, got)
		}
	})
}
