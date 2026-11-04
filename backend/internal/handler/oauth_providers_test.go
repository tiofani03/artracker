package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleExchange(t *testing.T) {
	verified := "true"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "c1" || r.Form.Get("grant_type") != "authorization_code" {
				http.Error(w, "bad", 400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at"}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer at" {
				http.Error(w, "unauth", 401)
				return
			}
			_, _ = w.Write([]byte(`{"sub":"g-1","email":"g@x.io","email_verified":` + verified + `,"name":"G","picture":"http://p"}`))
		}
	}))
	defer srv.Close()

	p := &googleProvider{clientID: "id", clientSecret: "s", tokenURL: srv.URL + "/token", userinfoURL: srv.URL + "/userinfo", client: srv.Client()}
	prof, err := p.Exchange(context.Background(), "c1", "http://r")
	if err != nil || prof.SSOID != "g-1" || prof.Email != "g@x.io" || prof.Provider != "google" {
		t.Fatalf("profile = %+v, err = %v", prof, err)
	}

	verified = "false"
	if _, err := p.Exchange(context.Background(), "c1", "http://r"); err == nil {
		t.Fatal("unverified google email must be rejected")
	}
	if !strings.Contains(p.AuthURL("st", "http://r"), "state=st") {
		t.Error("AuthURL must carry state")
	}
}
