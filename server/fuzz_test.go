package server

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/sirupsen/logrus"

	"github.com/syeo66/subsoxy/credentials"
)

func newFormTestServer() *ProxyServer {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	return &ProxyServer{logger: logger}
}

// newFormRequest builds the request by hand, since httptest.NewRequest panics on the
// malformed queries the fuzzer generates. The body is read a few bytes at a time to
// exercise partial reads.
func newFormRequest(post bool, query, contentType string, body []byte) *http.Request {
	method := http.MethodGet
	if post {
		method = http.MethodPost
	}
	r := &http.Request{
		Method: method,
		URL:    &url.URL{Path: "/rest/ping", RawQuery: query},
		Header: http.Header{},
		Body:   io.NopCloser(iotest.HalfReader(bytes.NewReader(body))),
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

// FuzzParseForm checks that parsing the form leaves the body intact for upstream, and that
// r.Form holds the body's values followed by the query's.
func FuzzParseForm(f *testing.F) {
	f.Add(true, "u=alice&f=json", "application/x-www-form-urlencoded", []byte("p=secret&id=1&id=2"))
	f.Add(true, "u=alice", "application/x-www-form-urlencoded; charset=utf-8", []byte("u=bob"))
	f.Add(true, "", "application/x-www-form-urlencoded", []byte("a=%zz&b=1"))
	f.Add(true, "a=1;b=2", "multipart/form-data; boundary=x", []byte("--x--"))
	f.Add(false, "u=alice&p=%ff", "application/x-www-form-urlencoded", []byte("p=other"))
	f.Add(true, "%", "", []byte{0xff, 0x00})

	ps := newFormTestServer()
	f.Fuzz(func(t *testing.T, post bool, query, contentType string, body []byte) {
		r := newFormRequest(post, query, contentType, body)
		ps.parseForm(r)

		forwarded, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading the body after parseForm: %v", err)
		}
		if !bytes.Equal(forwarded, body) {
			t.Fatalf("parseForm changed the body: %q became %q", body, forwarded)
		}

		postForm := url.Values{}
		if post && len(body) > 0 {
			if mediaType, _, _ := mime.ParseMediaType(contentType); mediaType == "application/x-www-form-urlencoded" {
				if parsed, err := url.ParseQuery(string(body)); err == nil {
					postForm = parsed
				}
			}
		}
		if !equalValues(r.PostForm, postForm) {
			t.Fatalf("PostForm = %v, want %v", r.PostForm, postForm)
		}

		queryValues := r.URL.Query()
		for key := range mergeKeys(postForm, queryValues) {
			want := slices.Concat(postForm[key], queryValues[key])
			if !slices.Equal(r.Form[key], want) {
				t.Fatalf("Form[%q] = %q, want %q (body first, then query)", key, r.Form[key], want)
			}
		}
		if len(r.Form) != len(mergeKeys(postForm, queryValues)) {
			t.Fatalf("Form has keys from neither body nor query: %v", r.Form)
		}
	})
}

// FuzzExtractCredentials checks the precedence of the credential sources, and that a
// credential taken from the form belongs to the "u" the hooks act on.
func FuzzExtractCredentials(f *testing.F) {
	f.Add(false, "u=alice&p=secret", "", "", "", "")
	f.Add(true, "u=alice", "p=secret&t=tok&s=salt", "", "", "")
	f.Add(false, "u=alice&t=tok&s=salt", "", "Basic Ym9iOnB3", "", "")
	f.Add(false, "u=alice", "", "Basic Ym9iOnB3", "carol", "pw")
	f.Add(false, "", "", "Basic OnB3", "", "")
	f.Add(false, "", "", "basic Ym9iOnB3", "carol", "pw")

	ps := newFormTestServer()
	f.Fuzz(func(t *testing.T, post bool, query, body, authorization, headerUser, headerPassword string) {
		r := newFormRequest(post, query, "application/x-www-form-urlencoded", []byte(body))
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		if headerUser != "" {
			r.Header.Set("X-Subsonic-Username", headerUser)
		}
		if headerPassword != "" {
			r.Header.Set("X-Subsonic-Password", headerPassword)
		}

		username, cred := ps.extractCredentials(r)

		u, p, tok, s := r.Form.Get("u"), r.Form.Get("p"), r.Form.Get("t"), r.Form.Get("s")
		switch {
		case u != "" && p != "":
			if username != u || cred != credentials.Password(p) {
				t.Fatalf("form has u and p, got %q %+v", username, cred)
			}
			return
		case u != "" && tok != "" && s != "":
			if username != u || cred != credentials.Token(tok, s) {
				t.Fatalf("form has u, t and s, got %q %+v", username, cred)
			}
			return
		}

		// Otherwise the credential comes from the headers, or there is none
		basicUser, basicPassword, basicOK := r.BasicAuth()
		basicOK = basicOK && strings.HasPrefix(authorization, "Basic ")
		switch {
		case basicOK:
			if username != basicUser || cred != credentials.Password(basicPassword) {
				t.Fatalf("Basic auth for %q, got %q %+v", basicUser, username, cred)
			}
		case headerUser != "" && headerPassword != "":
			if username != headerUser || cred != credentials.Password(headerPassword) {
				t.Fatalf("X-Subsonic headers for %q, got %q %+v", headerUser, username, cred)
			}
		default:
			if username != "" || cred != (credentials.Credential{}) {
				t.Fatalf("no credentials sent, got %q %+v", username, cred)
			}
		}
	})
}

// TestParseFormOversizedBody checks that a form body over MaxFormBodySize is left out of
// the form but still forwarded whole.
func TestParseFormOversizedBody(t *testing.T) {
	body := append([]byte("p=secret&"), bytes.Repeat([]byte("a"), MaxFormBodySize)...)
	r := newFormRequest(true, "u=alice", "application/x-www-form-urlencoded", body)
	newFormTestServer().parseForm(r)

	if len(r.PostForm) != 0 {
		t.Errorf("PostForm has %d keys, want none for an oversized body", len(r.PostForm))
	}
	if got := r.Form.Get("u"); got != "alice" {
		t.Errorf("Form u = %q, want the query's alice", got)
	}
	forwarded, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwarded, body) {
		t.Errorf("forwarded %d bytes, want the full %d", len(forwarded), len(body))
	}
}

func equalValues(a, b url.Values) bool {
	if len(a) != len(b) {
		return false
	}
	for key, values := range a {
		if !slices.Equal(values, b[key]) {
			return false
		}
	}
	return true
}

func mergeKeys(a, b url.Values) map[string]struct{} {
	keys := map[string]struct{}{}
	for key := range a {
		keys[key] = struct{}{}
	}
	for key := range b {
		keys[key] = struct{}{}
	}
	return keys
}
