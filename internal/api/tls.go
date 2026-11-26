package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxTLSBody is the largest body of POST /api/tls/self-signed: twenty names and two numbers.
const maxTLSBody = 8 << 10

// TLSStatus is the certificate the panel is set to serve. It holds no key and no path.
type TLSStatus struct {
	Enabled     bool      `json:"enabled"`
	SelfSigned  bool      `json:"selfSigned"`
	Subject     string    `json:"subject"`
	Names       []string  `json:"names"`
	NotBefore   time.Time `json:"notBefore"`
	NotAfter    time.Time `json:"notAfter"`
	Fingerprint string    `json:"fingerprint"`
}

// TLSResult is what POST /api/tls/self-signed answers. NeedRestart is true when the running
// service does not serve https from these files yet: the listener changes only at start.
type TLSResult struct {
	Subject     string    `json:"subject"`
	Names       []string  `json:"names"`
	NotAfter    time.Time `json:"notAfter"`
	Fingerprint string    `json:"fingerprint"`
	NeedRestart bool      `json:"needRestart"`
}

// TLSRefusal is a request the service refuses, with a sentence for the person (400). Any other
// error from the service is a server error.
type TLSRefusal string

func (e TLSRefusal) Error() string { return string(e) }

// TLSConflict is a state that stops the request, with a sentence for the person (409).
type TLSConflict string

func (e TLSConflict) Error() string { return string(e) }

// TLSService reads and makes the certificate. *tlsService in cmd/netscribe implements it.
type TLSService interface {
	// Status describes the certificate of the config. Enabled is false when none is set.
	Status(ctx context.Context) (TLSStatus, error)
	// GenerateSelfSigned makes the files next to the config and sets the two config keys. It
	// never restarts the service and never returns the key. Days 0 means the default and no
	// names means the names of this host. A bad name or length is a TLSRefusal; a certificate
	// that is set already, without replace, is a TLSConflict.
	GenerateSelfSigned(ctx context.Context, names []string, days int, replace bool) (TLSResult, error)
}

// getTLS answers GET /api/tls.
func (s *server) getTLS(w http.ResponseWriter, r *http.Request) {
	st, err := s.tls.Status(r.Context())
	if err != nil {
		s.internalError(w, "read certificate", err)
		return
	}
	if st.Names == nil {
		st.Names = []string{}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, st)
}

// postSelfSigned answers POST /api/tls/self-signed. It asks for the password again: a stolen
// session must not be enough to change what the panel serves.
func (s *server) postSelfSigned(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Names   []string `json:"names"`
		Days    int      `json:"days"`
		Replace bool     `json:"replace"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTLSBody))
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "the request body is not valid")
		return
	}
	if !s.confirmPassword(w, r, "tls_generate") {
		return
	}
	s.tlsMu.Lock()
	res, err := s.tls.GenerateSelfSigned(r.Context(), body.Names, body.Days, body.Replace)
	s.tlsMu.Unlock()
	var refusal TLSRefusal
	var conflict TLSConflict
	switch {
	case errors.As(err, &refusal):
		s.record(r, "tls_generate", "denied", string(refusal))
		writeError(w, http.StatusBadRequest, string(refusal))
	case errors.As(err, &conflict):
		s.record(r, "tls_generate", "denied", string(conflict))
		writeError(w, http.StatusConflict, string(conflict))
	case err != nil:
		s.record(r, "tls_generate", "error", "")
		s.internalError(w, "make certificate", err)
	default:
		s.record(r, "tls_generate", "ok", "fingerprint "+res.Fingerprint+"; names "+strings.Join(res.Names, ", "))
		if res.Names == nil {
			res.Names = []string{}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, res)
	}
}
