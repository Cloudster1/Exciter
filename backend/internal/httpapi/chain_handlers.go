package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// A project's trace model and the chain report that follows it (#151). See docs/api-contract-m5.md.

// chainError maps the trace model's and the chain report's refusals; nil when err is none of theirs.
func chainError(err error) *APIError {
	var (
		many  *trace.TooManyLinesError
		query *trace.QueryError
	)
	switch {
	case errors.As(err, &query):
		return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: query.Message, Fields: map[string]string{query.Field: query.Message}}
	case errors.As(err, &many):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: many.Error()}
	case errors.Is(err, trace.ErrNotModelKeeper):
		return ErrForbidden("Only the project's administrators set its trace model. Ask one of them.")
	}
	return nil
}

func (s *Server) handleGetTraceModel(w http.ResponseWriter, r *http.Request) {
	model, err := s.Trace.Model(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"model": model})
}

func (s *Server) handleSaveTraceModel(w http.ResponseWriter, r *http.Request) {
	var req trace.ModelInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	model, lsn, err := s.Trace.SaveModel(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"model": model})
}

// chains reads the chain report a request asks for: a window of its roots, or for a file every root up to the
// export's lines, cut there.
func (s *Server) chains(r *http.Request, q trace.ChainQuery) (*trace.Chains, error) {
	filter, sort, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		return nil, err
	}
	v := r.URL.Query()
	for _, raw := range v["level"] {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, &trace.FieldError{Field: "level", Message: "Name a level by its position, counted from 0."}
		}
		q.Levels = append(q.Levels, n)
	}
	switch v.Get("onlyBroken") {
	case "", "false":
	case "true":
		q.OnlyBroken = true
	default:
		return nil, &trace.FieldError{Field: "onlyBroken", Message: "Say true to keep only the broken chains, or false."}
	}
	q.Filter, q.Sort = filter, sort
	return s.Trace.Chains(r.Context(), userFrom(r), projectKeyParam(r), q)
}

func (s *Server) handleTraceChains(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, trace.ChainDefaultLimit, trace.ChainMaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	c, err := s.chains(r, trace.ChainQuery{Limit: limit, Offset: offset})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"chains": c})
}

// handleTraceChainsCSV writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) handleTraceChainsCSV(w http.ResponseWriter, r *http.Request) {
	c, err := s.chains(r, trace.ChainQuery{Limit: csvio.MaxExportRows, MaxLines: csvio.MaxExportRows, Truncate: true})
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.titleChainPages(r, c)
	var buf bytes.Buffer
	buf.Write(utf8Mark)
	if err := csvio.WriteChains(&buf, c); err != nil {
		respondError(w, r, err)
		return
	}
	download(w, "text/csv; charset=utf-8", requirement.NormalizedProjectKey(projectKeyParam(r))+"-chains.csv")
	if c.Truncated || len(c.Roots) < c.Total {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// titleChainPages names the pages a file's chains reach as the reader sees them in Stator.
func (s *Server) titleChainPages(r *http.Request, c *trace.Chains) {
	var ids []uuid.UUID
	for _, l := range c.Lines {
		for _, n := range l.Nodes {
			if n.Page != nil {
				ids = append(ids, n.Page.ID)
			}
		}
	}
	titles := s.pageTitles(r, ids)
	for _, l := range c.Lines {
		for _, n := range l.Nodes {
			if n.Page != nil {
				n.Page.Title = titles[n.Page.ID]
			}
		}
	}
}
