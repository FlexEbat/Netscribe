package api

import (
	"net/http"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// listContainers answers GET /api/containers with the containers of every host and
// whether the Docker daemon can be read right now. The list is what was stored last, so
// the page can still show it and say why it may be out of date.
func (s *server) listContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := s.containers.ListContainers(r.Context())
	if err != nil {
		s.internalError(w, "list containers", err)
		return
	}
	state := collector.DockerState{Enabled: true, Available: true}
	if s.docker != nil {
		state = s.docker.State()
	}
	writeJSON(w, http.StatusOK, struct {
		Containers []model.Container     `json:"containers"`
		Docker     collector.DockerState `json:"docker"`
	}{containers, state})
}
