package httpapi

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// A requirement's trace graph and the impact of changing it (#211). See docs/api-contract-m11.md.

// graphQueryOf reads what a graph is asked for from the query string; the service checks the values.
func graphQueryOf(r *http.Request) (trace.GraphQuery, error) {
	v := r.URL.Query()
	q := trace.GraphQuery{Direction: trace.GraphDirection(v.Get("direction")), TypeKeys: v["type"], Relations: v["relation"], Branch: v.Get("branch")}
	if raw := v.Get("depth"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return q, &trace.QueryError{Field: "depth", Message: "Give the depth as a number, such as 2."}
		}
		q.Depth = n
	}
	return q, nil
}

func (s *Server) graph(r *http.Request) (*trace.Graph, error) {
	q, err := graphQueryOf(r)
	if err != nil {
		return nil, err
	}
	return s.Trace.Graph(r.Context(), userFrom(r), reqKeyParam(r), q)
}

func (s *Server) handleTraceGraph(w http.ResponseWriter, r *http.Request) {
	g, err := s.graph(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"graph": g})
}

// handleTraceGraphCSV writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) handleTraceGraphCSV(w http.ResponseWriter, r *http.Request) {
	g, err := s.graph(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	var ids []uuid.UUID
	for _, n := range g.Nodes {
		if n.Page != nil {
			ids = append(ids, n.Page.ID)
		}
	}
	titles := s.pageTitles(r, ids)
	for _, n := range g.Nodes {
		if n.Page != nil {
			n.Page.Title = titles[n.Page.ID]
		}
	}
	var buf bytes.Buffer
	buf.Write(utf8Mark)
	if err := csvio.WriteImpact(&buf, g); err != nil {
		respondError(w, r, err)
		return
	}
	download(w, "text/csv; charset=utf-8", requirement.NormalizedProjectKey(reqKeyParam(r))+"-impact.csv")
	if g.Truncated {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
