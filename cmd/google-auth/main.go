// Command google-auth runs a one-time OAuth user-consent flow to mint a Google
// Calendar offline refresh token for ruby-core (ROADMAP-0012, ADR-0042). Run it
// once on the host: sign in as the household account, grant access, and it prints
// the refresh token to paste into Vault at secret/ruby-core/google.
//
// The OAuth app MUST be in PRODUCTION publishing status — refresh tokens issued
// while the consent screen is in "Testing" status are revoked by Google after
// ~7 days. Use a Desktop-app OAuth client (loopback redirect). See
// docs/runbooks/google-calendar-oauth.md.
//
// Usage:
//
//	go run ./cmd/google-auth --client-id <id> --client-secret <secret>
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// calendarEventsScope is the minimal scope ruby-core needs: read/write events on
// calendars the user can access (not full calendar management).
const calendarEventsScope = "https://www.googleapis.com/auth/calendar.events"

func main() {
	clientID := flag.String("client-id", "", "Google OAuth client ID (Desktop app)")
	clientSecret := flag.String("client-secret", "", "Google OAuth client secret")
	port := flag.Int("port", 8765, "loopback port for the OAuth redirect")
	flag.Parse()

	if *clientID == "" || *clientSecret == "" {
		log.Fatal("both --client-id and --client-secret are required")
	}

	conf := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: *clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  fmt.Sprintf("http://127.0.0.1:%d/callback", *port),
		Scopes:       []string{calendarEventsScope},
	}

	state := randomState()
	// AccessTypeOffline asks for a refresh token; ApprovalForce sets prompt=consent
	// so Google re-issues the refresh token even if the user consented before.
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	type result struct {
		token *oauth2.Token
		err   error
	}
	resCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			resCh <- result{err: fmt.Errorf("state mismatch (possible CSRF)")}
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, "consent denied: "+e, http.StatusBadRequest)
			resCh <- result{err: fmt.Errorf("consent denied: %s", e)}
			return
		}
		tok, err := conf.Exchange(r.Context(), q.Get("code"))
		if err != nil {
			http.Error(w, "token exchange failed: "+err.Error(), http.StatusInternalServerError)
			resCh <- result{err: err}
			return
		}
		_, _ = fmt.Fprintln(w, "Success — you may close this tab and return to the terminal.")
		resCh <- result{token: tok}
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("listen on 127.0.0.1:%d: %v", *port, err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	fmt.Println("Open this URL in a browser signed in as the household Google account, then grant access:")
	fmt.Println()
	fmt.Println("   " + authURL)
	fmt.Println()
	fmt.Println("Waiting for consent (5 minute timeout)...")

	select {
	case res := <-resCh:
		_ = srv.Shutdown(context.Background())
		if res.err != nil {
			log.Fatalf("auth failed: %v", res.err)
		}
		if res.token.RefreshToken == "" {
			log.Fatal("no refresh_token returned. Google only issues one on first consent; " +
				"revoke the app at https://myaccount.google.com/permissions and re-run.")
		}
		fmt.Println()
		fmt.Println("refresh_token:")
		fmt.Println("  " + res.token.RefreshToken)
		fmt.Println()
		fmt.Println("Store it in Vault (see docs/runbooks/google-calendar-oauth.md):")
		fmt.Println("  vault kv put secret/ruby-core/google \\")
		fmt.Println("    client_id=<id> client_secret=<secret> refresh_token=<above> calendar_id=<id>")
	case <-time.After(5 * time.Minute):
		_ = srv.Shutdown(context.Background())
		log.Fatal("timed out waiting for consent")
	}
}

func randomState() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
