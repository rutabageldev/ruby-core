package gcal

import (
	"context"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"

	"github.com/primaryrutabaga/ruby-core/pkg/boot"
)

// calendarEventsScope is the scope ruby-core consents to (read/write events).
const calendarEventsScope = "https://www.googleapis.com/auth/calendar.events"

// TokenSource returns an auto-refreshing token source backed by the Vault-stored
// offline refresh token. ReuseTokenSource caches the access token until it expires,
// then the underlying source refreshes it using the refresh token (ADR-0042).
func TokenSource(ctx context.Context, cfg *boot.GoogleConfig) oauth2.TokenSource {
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     googleoauth.Endpoint,
		Scopes:       []string{calendarEventsScope},
	}
	tok := &oauth2.Token{RefreshToken: cfg.RefreshToken}
	return oauth2.ReuseTokenSource(tok, oc.TokenSource(ctx, tok))
}
