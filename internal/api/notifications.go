package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/FlexEbat/Netscribe/internal/notify"
)

// Notifications is the part of the notifier that the API uses. *notify.Notifier is the one
// implementation.
type Notifications interface {
	Channels() []notify.ChannelInfo
	Test(ctx context.Context, name string) error
}

// listNotifications answers GET /api/notifications with {"channels": [...]}. A channel
// shows its name, its type and the events it is sent, never an address or a secret.
func (s *server) listNotifications(w http.ResponseWriter, _ *http.Request) {
	channels := s.notifier.Channels()
	if channels == nil {
		channels = []notify.ChannelInfo{}
	}
	writeJSON(w, http.StatusOK, map[string][]notify.ChannelInfo{"channels": channels})
}

type testNotificationBody struct {
	Channel string `json:"channel"`
}

// testNotification answers POST /api/notifications/test: 200 when the message was
// delivered, 400 for a body without a channel, 404 for a channel that is not configured,
// 429 when the same channel was tested a moment ago and 502 when the delivery failed. The
// text of a delivery error never holds an address or a token.
func (s *server) testNotification(w http.ResponseWriter, r *http.Request) {
	var body testNotificationBody
	if !decode(r, &body) || body.Channel == "" {
		writeError(w, http.StatusBadRequest, "channel is required")
		return
	}
	switch err := s.notifier.Test(r.Context(), body.Channel); {
	case errors.Is(err, notify.ErrUnknownChannel):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, notify.ErrTooSoon):
		w.Header().Set("Retry-After", "10")
		writeError(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		s.record(r, "notify_test", "error", body.Channel)
		writeError(w, http.StatusBadGateway, "the message was not delivered: "+err.Error())
	default:
		s.record(r, "notify_test", "ok", body.Channel)
		writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}
