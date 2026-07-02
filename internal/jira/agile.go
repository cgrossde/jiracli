package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cgrossde/jiracli/internal/cache"
)

// ErrBoardNoSprints is returned when a board does not support sprints (kanban boards).
var ErrBoardNoSprints = errors.New("board does not support sprints")

// agileURL builds the full URL for a Jira Agile REST API 1.0 path.
// path must start with "/" (e.g. "/board").
func (c *Client) agileURL(path string, query url.Values) string {
	u := c.BaseURL + "/rest/agile/1.0" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// AgileGet performs GET /rest/agile/1.0<path>?<query>.
func (c *Client) AgileGet(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.agileURL(path, query), nil)
	if err != nil {
		return nil, 0, err
	}
	return c.do(req)
}

// AgilePost performs POST /rest/agile/1.0<path>?<query> with the given body.
func (c *Client) AgilePost(ctx context.Context, path string, query url.Values, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.agileURL(path, query), body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// AgilePut performs PUT /rest/agile/1.0<path>?<query> with the given body.
func (c *Client) AgilePut(ctx context.Context, path string, query url.Values, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.agileURL(path, query), body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// isBoardNoSprints reports whether a 400 response body contains the kanban rejection message.
func isBoardNoSprints(body []byte) bool {
	return strings.Contains(string(body), "The board doesn't support sprints.")
}

// agileEnvelope is the paged wrapper returned by most Agile list endpoints.
type agileEnvelope struct {
	Values     json.RawMessage `json:"values"`
	StartAt    int             `json:"startAt"`
	MaxResults int             `json:"maxResults"`
	IsLast     bool            `json:"isLast"`
	Total      int             `json:"total"`
}

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// Board is a Jira Agile board (scrum or kanban).
type Board struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "scrum" | "kanban"
}

// BoardColumn is one column in a board's column configuration.
type BoardColumn struct {
	Name      string   `json:"name"`
	StatusIDs []string `json:"statusIds"` // status IDs
}

// BoardConfig is the full configuration of a board, including columns.
type BoardConfig struct {
	ID       int           `json:"id"`
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	FilterID string        `json:"filterId,omitempty"`
	Columns  []BoardColumn `json:"columns"`
}

// BoardFilter holds the metadata for the Jira saved filter backing a board.
type BoardFilter struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	JQL         string `json:"jql"`
	OwnerName   string `json:"ownerName"`
	OwnerActive bool   `json:"ownerActive"`
	Editable    bool   `json:"editable"`
}

// Sprint is a Jira Agile sprint.
type Sprint struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	State         string `json:"state"` // "active" | "future" | "closed"
	StartDate     string `json:"startDate,omitempty"`
	EndDate       string `json:"endDate,omitempty"`
	ActivatedDate string `json:"activatedDate,omitempty"`
	OriginBoardID int    `json:"originBoardId,omitempty"`
	Goal          string `json:"goal,omitempty"`

	// Sequence is GreenHopper's board-position/creation-order counter,
	// populated only via ListSprintNames (the legacy sprintquery endpoint —
	// see its doc comment). It is a much better chronology proxy than ID
	// when real dates aren't available: on real Jira DC instances, sprint
	// IDs are not reliably chronological (board migrations, cross-project ID
	// pools, renumbering), while Sequence tracks true creation order far
	// more closely. It is still an approximation, not a guarantee — see
	// docs/boards-sprints.md#sprint-ordering-caveats for measured error
	// rates. Zero when unavailable (e.g. sprints sourced from
	// ListAllSprintsPaged, which doesn't carry this field).
	Sequence int `json:"sequence,omitempty"`
}

// AgileConfig is per-instance Agile configuration, mirroring keychain.AgileConfig.
// Used within the jira package for ergonomic access.
type AgileConfig struct {
	SprintField  string    `json:"sprintField,omitempty"`
	DiscoveredAt time.Time `json:"discoveredAt,omitempty"`
}

// ---------------------------------------------------------------------------
// Boards
// ---------------------------------------------------------------------------

// ListBoards fetches boards for a project key, page-by-page.
// GET /board?projectKeyOrId=<KEY>&startAt=&maxResults=
func (c *Client) ListBoards(ctx context.Context, projectKey string, page, limit int) ([]Board, int, error) {
	q := url.Values{}
	q.Set("projectKeyOrId", projectKey)
	q.Set("startAt", fmt.Sprintf("%d", (page-1)*limit))
	q.Set("maxResults", fmt.Sprintf("%d", limit))
	body, status, err := c.AgileGet(ctx, "/board", q)
	if err != nil {
		return nil, 0, err
	}
	if status != 200 {
		return nil, 0, fmt.Errorf("list boards: %w", MapStatus("", status, body))
	}
	var env agileEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, 0, fmt.Errorf("parse boards: %w", err)
	}
	var boards []Board
	if len(env.Values) > 0 && string(env.Values) != "null" {
		if err := json.Unmarshal(env.Values, &boards); err != nil {
			return nil, 0, fmt.Errorf("parse boards values: %w", err)
		}
	}
	return boards, env.Total, nil
}

// ListBoardsCached wraps ListBoards with caching.
// Cache key: "boards/<projectKey>", TTL 1h — only on page 1, limit >= 50.
func (c *Client) ListBoardsCached(ctx context.Context, projectKey string, page, limit int, store *cache.Store, noCache bool) ([]Board, int, error) {
	type cached struct {
		Boards []Board `json:"boards"`
		Total  int     `json:"total"`
	}
	cacheKey := "boards/" + projectKey
	if !noCache && store != nil && page == 1 && limit >= 50 {
		var cv cached
		if err := store.Get(cacheKey, TTLBoards, &cv); err == nil {
			return cv.Boards, cv.Total, nil
		}
	}
	boards, total, err := c.ListBoards(ctx, projectKey, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if !noCache && store != nil && page == 1 && limit >= 50 {
		_ = store.Put(cacheKey, cached{Boards: boards, Total: total})
	}
	return boards, total, nil
}

// GetBoardConfig fetches the configuration of a board.
// GET /board/{id}/configuration
func (c *Client) GetBoardConfig(ctx context.Context, boardID int) (BoardConfig, error) {
	body, status, err := c.AgileGet(ctx, fmt.Sprintf("/board/%d/configuration", boardID), nil)
	if err != nil {
		return BoardConfig{}, err
	}
	if status != 200 {
		return BoardConfig{}, fmt.Errorf("board config: %w", MapStatus("", status, body))
	}
	var raw struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Filter struct {
			ID string `json:"id"`
		} `json:"filter"`
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return BoardConfig{}, fmt.Errorf("parse board config: %w", err)
	}
	cfg := BoardConfig{
		ID:       raw.ID,
		Name:     raw.Name,
		Type:     raw.Type,
		FilterID: raw.Filter.ID,
	}
	for _, col := range raw.ColumnConfig.Columns {
		bc := BoardColumn{Name: col.Name}
		for _, s := range col.Statuses {
			bc.StatusIDs = append(bc.StatusIDs, s.ID)
		}
		cfg.Columns = append(cfg.Columns, bc)
	}
	return cfg, nil
}

// GetBoardConfigCached wraps GetBoardConfig with caching.
// Cache key: "board/<id>/config", TTL 1h.
func (c *Client) GetBoardConfigCached(ctx context.Context, boardID int, store *cache.Store, noCache bool) (BoardConfig, error) {
	cacheKey := fmt.Sprintf("board/%d/config", boardID)
	if !noCache && store != nil {
		var cfg BoardConfig
		if err := store.Get(cacheKey, TTLBoardConfig, &cfg); err == nil {
			return cfg, nil
		}
	}
	cfg, err := c.GetBoardConfig(ctx, boardID)
	if err != nil {
		return BoardConfig{}, err
	}
	if !noCache && store != nil {
		_ = store.Put(cacheKey, cfg)
	}
	return cfg, nil
}

// GetBoardFilter fetches the saved filter backing a board.
// GET /rest/api/2/filter/{id}
// Returns the filter name, JQL, owner display name, and whether it is editable by the caller.
func (c *Client) GetBoardFilter(ctx context.Context, filterID string) (BoardFilter, error) {
	body, status, err := c.Get(ctx, fmt.Sprintf("/filter/%s", filterID), nil)
	if err != nil {
		return BoardFilter{}, err
	}
	if status != 200 {
		return BoardFilter{}, fmt.Errorf("board filter: %w", MapStatus("", status, body))
	}
	var raw struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		JQL      string `json:"jql"`
		Editable bool   `json:"editable"`
		Owner    struct {
			DisplayName string `json:"displayName"`
			Active      bool   `json:"active"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return BoardFilter{}, fmt.Errorf("parse board filter: %w", err)
	}
	return BoardFilter{
		ID:          raw.ID,
		Name:        raw.Name,
		JQL:         raw.JQL,
		OwnerName:   raw.Owner.DisplayName,
		OwnerActive: raw.Owner.Active,
		Editable:    raw.Editable,
	}, nil
}

// ---------------------------------------------------------------------------
// Sprints
// ---------------------------------------------------------------------------

// ListSprints fetches sprints for a board, filtered by states.
// GET /board/{id}/sprint?state=<csv>&startAt=&maxResults=
// states == nil omits the state filter (returns all).
// Returns ErrBoardNoSprints on kanban boards.
func (c *Client) ListSprints(ctx context.Context, boardID int, states []string, page, limit int) ([]Sprint, bool, error) {
	q := url.Values{}
	if len(states) > 0 {
		q.Set("state", strings.Join(states, ","))
	}
	q.Set("startAt", fmt.Sprintf("%d", (page-1)*limit))
	q.Set("maxResults", fmt.Sprintf("%d", limit))
	body, status, err := c.AgileGet(ctx, fmt.Sprintf("/board/%d/sprint", boardID), q)
	if err != nil {
		return nil, false, err
	}
	if status == 400 && isBoardNoSprints(body) {
		return nil, false, fmt.Errorf("list sprints: %w", ErrBoardNoSprints)
	}
	if status != 200 {
		return nil, false, fmt.Errorf("list sprints: %w", MapStatus("", status, body))
	}
	var env agileEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, false, fmt.Errorf("parse sprints: %w", err)
	}
	var sprints []Sprint
	if len(env.Values) > 0 && string(env.Values) != "null" {
		if err := json.Unmarshal(env.Values, &sprints); err != nil {
			return nil, false, fmt.Errorf("parse sprints values: %w", err)
		}
	}
	return sprints, env.IsLast, nil
}

// ListSprintsCached wraps ListSprints with caching.
// Only caches well-known state combos on page 1:
//   - states==["active","future"] → TTLSprintsActive
//   - states==["closed"]          → TTLSprintsClosed
//
// The cached entry stores isLast alongside the sprints so a cache hit reports
// the same pagination signal as a live fetch.
func (c *Client) ListSprintsCached(ctx context.Context, boardID int, states []string, page, limit int, store *cache.Store, noCache bool) ([]Sprint, bool, error) {
	isActiveSet := len(states) == 2 && states[0] == "active" && states[1] == "future"
	isClosedSet := len(states) == 1 && states[0] == "closed"

	var cacheKey string
	var cacheTTL time.Duration
	if page == 1 && isActiveSet {
		cacheKey = fmt.Sprintf("sprints/%d/active+future", boardID)
		cacheTTL = TTLSprintsActive
	} else if page == 1 && isClosedSet {
		cacheKey = fmt.Sprintf("sprints/%d/closed", boardID)
		cacheTTL = TTLSprintsClosed
	}

	// cachedSprints wraps the page-1 result with its isLast signal. Entries
	// written under the old []Sprint shape fail to unmarshal here and are
	// treated as a miss, so the cache self-heals on the next fetch.
	type cachedSprints struct {
		Sprints []Sprint `json:"sprints"`
		IsLast  bool     `json:"isLast"`
	}

	if cacheKey != "" && !noCache && store != nil {
		var cv cachedSprints
		if err := store.Get(cacheKey, cacheTTL, &cv); err == nil {
			return cv.Sprints, cv.IsLast, nil
		}
	}
	sprints, isLast, err := c.ListSprints(ctx, boardID, states, page, limit)
	if err != nil {
		return nil, false, err
	}
	if cacheKey != "" && !noCache && store != nil {
		_ = store.Put(cacheKey, cachedSprints{Sprints: sprints, IsLast: isLast})
	}
	return sprints, isLast, nil
}

// sprintQueryEnvelope is the response of GET /rest/greenhopper/1.0/sprintquery/{boardID}.
// This legacy endpoint returns ALL sprints for a board in a single HTTP call.
// Dates are NOT included — caller must hydrate them via GetSprint when needed.
type sprintQueryEnvelope struct {
	Sprints []struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		State    string `json:"state"` // "CLOSED" | "ACTIVE" | "FUTURE"
		Sequence int    `json:"sequence"`
	} `json:"sprints"`
	RapidViewID int `json:"rapidViewId"`
}

// ListSprintNames fetches every sprint id+name+state for a board in one HTTP call
// via the legacy GreenHopper sprintquery endpoint. Dates are NOT included —
// caller must hydrate them via HydrateSprintDates when needed.
//
// Cached for 1h under "sprints/<board>/names". If the endpoint returns non-200
// (older DC version, plugin disabled), an error is returned and the caller should
// fall back to ListAllSprintsPaged.
func (c *Client) ListSprintNames(ctx context.Context, boardID int, store *cache.Store, noCache bool) ([]Sprint, error) {
	cacheKey := fmt.Sprintf("sprints/%d/names", boardID)
	if !noCache && store != nil {
		var cached []Sprint
		if err := store.Get(cacheKey, TTLSprintsActive, &cached); err == nil {
			return cached, nil
		}
	}
	u := c.BaseURL + fmt.Sprintf("/rest/greenhopper/1.0/sprintquery/%d?includeHistoricSprints=true&includeFutureSprints=true", boardID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("sprintquery board %d: %w", boardID, MapStatus("", status, body))
	}
	var env sprintQueryEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse sprintquery: %w", err)
	}
	out := make([]Sprint, 0, len(env.Sprints))
	for _, s := range env.Sprints {
		out = append(out, Sprint{
			ID:            s.ID,
			Name:          s.Name,
			State:         strings.ToLower(s.State), // normalize to lowercase to match agile/1.0
			OriginBoardID: env.RapidViewID,
			Sequence:      s.Sequence,
			// StartDate / EndDate intentionally empty — call HydrateSprintDates
			// (or, for closed sprints in bulk, BulkBackfillClosedDates) if needed.
		})
	}
	if !noCache && store != nil {
		_ = store.Put(cacheKey, out)
	}
	return out, nil
}

// HydrateSprintDates fills StartDate/EndDate on sprints that lack them by
// calling GetSprint per id. Existing dates are preserved (no overwrite).
// Failed individual lookups leave that sprint's dates empty and are silently skipped.
//
// Lookups run concurrently (bounded worker pool) because this is called on a
// single display page of sprints where sequential per-id round-trips would
// dominate latency. Each worker writes only its own unique slice index, so no
// locking is required.
func (c *Client) HydrateSprintDates(ctx context.Context, sprints []Sprint) []Sprint {
	const maxWorkers = 8

	todo := make([]int, 0, len(sprints))
	for i := range sprints {
		if sprints[i].StartDate == "" && sprints[i].EndDate == "" {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return sprints
	}

	workers := maxWorkers
	if len(todo) < workers {
		workers = len(todo)
	}
	idx := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range idx {
				full, err := c.GetSprint(ctx, sprints[i].ID)
				if err != nil {
					continue
				}
				sprints[i].StartDate = full.StartDate
				sprints[i].EndDate = full.EndDate
			}
		}()
	}
	for _, i := range todo {
		idx <- i
	}
	close(idx)
	wg.Wait()
	return sprints
}

// maxBulkBackfillPages bounds how many pages BulkBackfillClosedDates reads
// from the native paged closed-sprint endpoint. Each page holds up to 100
// sprints, so the default caps the call at 500 sprints — enough to fully
// cover the vast majority of boards in a small, constant number of
// round-trips, while keeping the worst case (a board with thousands of
// closed sprints) bounded rather than degenerating into per-sprint hydration.
const maxBulkBackfillPages = 5

// BulkBackfillClosedDates opportunistically fills StartDate/EndDate on
// dateless closed sprints using the native paged Agile endpoint
// (GET /board/{id}/sprint?state=closed), which returns real dates for every
// sprint it lists — no per-sprint round-trip needed, unlike GetSprint/
// HydrateSprintDates.
//
// This endpoint is deliberately NOT used as the primary source for the full
// closed-sprint history (see ListSprintNames's doc comment): it has been
// observed, empirically, to under-report on boards with long sprint
// histories — one production board returned only 50 of 55 closed sprints,
// silently dropping the other 5 with no error or truncation signal. Trusting
// it as a complete listing would silently corrupt totals elsewhere (e.g.
// --after/--before, effort rollups). As a bulk *backfill* for date
// enrichment that risk doesn't apply: sprints it happens to miss simply keep
// their zero dates, and the caller (sprint list's sort) falls back to
// Sequence-based ordering for those — see docs/boards-sprints.md#sprint-ordering-caveats.
//
// Bounded to maxBulkBackfillPages page reads regardless of board size, so
// cost is a small constant rather than O(number of closed sprints) the way
// per-sprint hydration is. Sprints that already carry a date, or aren't
// closed, are left untouched. Page 1 reuses ListSprintsCached (7-day TTL);
// subsequent pages (needed only on boards with >100 closed sprints) are not
// cached.
func (c *Client) BulkBackfillClosedDates(ctx context.Context, boardID int, sprints []Sprint, store *cache.Store, noCache bool) []Sprint {
	needed := make(map[int]int, len(sprints)) // sprint id -> index into sprints
	for i, s := range sprints {
		if strings.EqualFold(s.State, "closed") && s.StartDate == "" && s.EndDate == "" {
			needed[s.ID] = i
		}
	}
	if len(needed) == 0 {
		return sprints
	}

	for page := 1; page <= maxBulkBackfillPages; page++ {
		var batch []Sprint
		var isLast bool
		var err error
		if page == 1 {
			batch, isLast, err = c.ListSprintsCached(ctx, boardID, []string{"closed"}, 1, 100, store, noCache)
		} else {
			batch, isLast, err = c.ListSprints(ctx, boardID, []string{"closed"}, page, 100)
		}
		if err != nil {
			// Best-effort: leave remaining sprints dateless. The caller
			// falls back to Sequence/ID ordering for them.
			break
		}
		for _, b := range batch {
			if i, ok := needed[b.ID]; ok {
				sprints[i].StartDate = b.StartDate
				sprints[i].EndDate = b.EndDate
				delete(needed, b.ID)
			}
		}
		if isLast || len(batch) == 0 || len(needed) == 0 {
			break
		}
	}
	return sprints
}

// ListAllSprintsPaged fetches every sprint matching the state filter by paging
// through ListSprints until isLast=true. Page size 100 (API max).
// Used when dates are needed up front (--after / --before).
//
// When states == ["closed"] the result is cached for 7 days under
// "sprints/<board>/closed-all". Other state combinations are not cached.
func (c *Client) ListAllSprintsPaged(ctx context.Context, boardID int, states []string, store *cache.Store, noCache bool) ([]Sprint, error) {
	isClosedOnly := len(states) == 1 && states[0] == "closed"
	var cacheKey string
	if isClosedOnly {
		cacheKey = fmt.Sprintf("sprints/%d/closed-all", boardID)
	}
	if cacheKey != "" && !noCache && store != nil {
		var cached []Sprint
		if err := store.Get(cacheKey, TTLSprintsClosed, &cached); err == nil {
			return cached, nil
		}
	}
	var all []Sprint
	for page := 1; ; page++ {
		batch, isLast, err := c.ListSprints(ctx, boardID, states, page, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if isLast || len(batch) == 0 {
			break
		}
	}
	if cacheKey != "" && !noCache && store != nil {
		_ = store.Put(cacheKey, all)
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Recency window + archive cache
// ---------------------------------------------------------------------------

const (
	// DefaultClosedWindow is the default recency window for the `sprint list`
	// default view: closed sprints whose endDate falls within this window of now
	// are shown; older ones require --all or a date-range filter.
	DefaultClosedWindow = 7 * 24 * time.Hour

	// sprintArchiveAge marks the point past which a closed sprint is immutable.
	// A sprint closed longer than this ago never changes, so its fully hydrated
	// record is cached (near-)permanently and never refetched.
	sprintArchiveAge = 90 * 24 * time.Hour

	// closedProbeCap bounds how many closed sprints the default view will hydrate
	// while scanning newest-first for the recency window. It is a safety net for
	// boards whose sprint ids are not perfectly monotonic with end date; the scan
	// normally stops far earlier when it crosses the window boundary.
	closedProbeCap = 50
)

// parseSprintTime parses a Jira sprint date/datetime string, returning the zero
// time when s is empty or unparseable.
func parseSprintTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// isImmutableClosed reports whether a sprint is closed and ended long enough ago
// (> sprintArchiveAge) that its record will never change again.
func isImmutableClosed(s Sprint, now time.Time) bool {
	if !strings.EqualFold(s.State, "closed") {
		return false
	}
	end := parseSprintTime(s.EndDate)
	if end.IsZero() {
		return false
	}
	return now.Sub(end) > sprintArchiveAge
}

// loadSprintArchive reads the per-board archive of immutable closed sprints
// (id -> fully hydrated Sprint). A missing/expired/corrupt cache yields an empty
// map. The archive TTL is intentionally long: its members never change.
func (c *Client) loadSprintArchive(boardID int, store *cache.Store, noCache bool) map[int]Sprint {
	if noCache || store == nil {
		return map[int]Sprint{}
	}
	var cached map[int]Sprint
	if err := store.Get(fmt.Sprintf("sprints/%d/archive", boardID), TTLSprintArchive, &cached); err == nil && cached != nil {
		return cached
	}
	return map[int]Sprint{}
}

// saveSprintArchive persists the archive map. No-op when caching is disabled or
// the map is empty.
func (c *Client) saveSprintArchive(boardID int, store *cache.Store, noCache bool, arch map[int]Sprint) {
	if noCache || store == nil || len(arch) == 0 {
		return
	}
	_ = store.Put(fmt.Sprintf("sprints/%d/archive", boardID), arch)
}

// allSprintNames returns every sprint for a board (id/name/state), preferring the
// single-call GreenHopper sprintquery endpoint and falling back to the paged
// agile/1.0 endpoint when GreenHopper is unavailable. The bool reports whether
// the returned sprints already carry start/end dates (true only on the paged
// fallback path).
func (c *Client) allSprintNames(ctx context.Context, boardID int, store *cache.Store, noCache bool) ([]Sprint, bool, error) {
	names, err := c.ListSprintNames(ctx, boardID, store, noCache)
	if err == nil {
		return names, false, nil
	}
	slog.Warn("sprintquery unavailable, falling back to paged listing", "board", boardID, "err", err)
	paged, perr := c.ListAllSprintsPaged(ctx, boardID, nil, store, noCache)
	if perr != nil {
		return nil, false, perr
	}
	return paged, true, nil
}

// ListSprintsDefaultView returns the default `sprint list` working set for a
// board: every active and future sprint plus closed sprints whose endDate falls
// within closedWindow of now. Dates are hydrated on the returned sprints.
//
// Closed sprints are scanned newest-id-first (sprint id is a recency proxy) and
// hydrated one at a time only until one falls outside the window — everything
// older is then assumed outside it too, so hydration stops. Dates for immutable
// closed sprints (ended > 90d ago) are read from, and newly discovered ones
// written to, the archive cache. The whole view is cached for TTLSprintsActive
// so repeat calls within the hour avoid re-hydration entirely.
//
// This keeps the default view to a handful of round-trips even on boards with
// years of sprint history.
func (c *Client) ListSprintsDefaultView(ctx context.Context, boardID int, closedWindow time.Duration, store *cache.Store, noCache bool) ([]Sprint, error) {
	days := int(closedWindow / (24 * time.Hour))
	cacheKey := fmt.Sprintf("sprints/%d/default-%dd", boardID, days)
	if !noCache && store != nil {
		var cached []Sprint
		if err := store.Get(cacheKey, TTLSprintsActive, &cached); err == nil {
			return cached, nil
		}
	}

	all, haveDates, err := c.allSprintNames(ctx, boardID, store, noCache)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	cutoff := now.Add(-closedWindow)

	var live, closed []Sprint
	for _, s := range all {
		if strings.EqualFold(s.State, "closed") {
			closed = append(closed, s)
		} else {
			live = append(live, s)
		}
	}
	// Active + future are always shown; hydrate their (small) date set.
	if !haveDates {
		live = c.HydrateSprintDates(ctx, live)
	}

	// Closed sprints: newest id first, stop once one falls outside the window.
	sort.Slice(closed, func(i, j int) bool { return closed[i].ID > closed[j].ID })
	arch := c.loadSprintArchive(boardID, store, noCache)
	var recent []Sprint
	probes := 0
	for i := range closed {
		s := closed[i]
		if a, ok := arch[s.ID]; ok {
			// Immutable, already-hydrated old sprint — use cached dates, no fetch.
			s.StartDate, s.EndDate = a.StartDate, a.EndDate
		} else if !haveDates && s.StartDate == "" && s.EndDate == "" {
			full, gErr := c.GetSprint(ctx, s.ID)
			if gErr != nil {
				continue
			}
			s.StartDate, s.EndDate = full.StartDate, full.EndDate
			probes++
		}
		end := parseSprintTime(s.EndDate)
		if !end.IsZero() && end.Before(cutoff) {
			// Outside the window. Sprint ids are a recency proxy, so every
			// remaining (older) closed sprint is outside it too: stop here.
			if isImmutableClosed(s, now) {
				arch[s.ID] = s
			}
			break
		}
		if !end.IsZero() {
			recent = append(recent, s)
		}
		if probes >= closedProbeCap {
			break
		}
	}
	c.saveSprintArchive(boardID, store, noCache, arch)

	out := append(live, recent...)
	if !noCache && store != nil {
		_ = store.Put(cacheKey, out)
	}
	return out, nil
}

// ListAllSprintsHydrated returns every sprint for a board with start/end dates
// hydrated. It is the complete set (via the GreenHopper single-call endpoint),
// unlike the paged agile/1.0 closed endpoint which can under-report on boards
// with long histories. Immutable closed sprints are served from — and newly
// discovered ones added to — the archive cache, so repeat calls only hydrate the
// handful of sprints that can still change (active/future/recently-closed).
func (c *Client) ListAllSprintsHydrated(ctx context.Context, boardID int, store *cache.Store, noCache bool) ([]Sprint, error) {
	all, haveDates, err := c.allSprintNames(ctx, boardID, store, noCache)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	arch := c.loadSprintArchive(boardID, store, noCache)

	if !haveDates {
		for i := range all {
			if a, ok := arch[all[i].ID]; ok {
				all[i].StartDate = a.StartDate
				all[i].EndDate = a.EndDate
			}
		}
		all = c.HydrateSprintDates(ctx, all) // fills only the still-empty ones
	}

	changed := false
	for i := range all {
		if _, ok := arch[all[i].ID]; ok {
			continue
		}
		if isImmutableClosed(all[i], now) {
			arch[all[i].ID] = all[i]
			changed = true
		}
	}
	if changed {
		c.saveSprintArchive(boardID, store, noCache, arch)
	}
	return all, nil
}

// GetSprint fetches a single sprint by ID.
// GET /sprint/{id}
func (c *Client) GetSprint(ctx context.Context, sprintID int) (Sprint, error) {
	body, status, err := c.AgileGet(ctx, fmt.Sprintf("/sprint/%d", sprintID), nil)
	if err != nil {
		return Sprint{}, err
	}
	if status != 200 {
		return Sprint{}, fmt.Errorf("get sprint: %w", MapStatus("", status, body))
	}
	var s Sprint
	if err := json.Unmarshal(body, &s); err != nil {
		return Sprint{}, fmt.Errorf("parse sprint: %w", err)
	}
	return s, nil
}

// ListSprintIssues fetches issues in a sprint via the Agile API.
// GET /sprint/{id}/issue?startAt=&maxResults=&fields=
func (c *Client) ListSprintIssues(ctx context.Context, sprintID, page, limit int, fields []string) (SearchResponse, error) {
	q := url.Values{}
	q.Set("startAt", fmt.Sprintf("%d", (page-1)*limit))
	q.Set("maxResults", fmt.Sprintf("%d", limit))
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	body, status, err := c.AgileGet(ctx, fmt.Sprintf("/sprint/%d/issue", sprintID), q)
	if err != nil {
		return SearchResponse{}, err
	}
	if status != 200 {
		return SearchResponse{}, fmt.Errorf("sprint issues: %w", MapStatus("", status, body))
	}
	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return SearchResponse{}, fmt.Errorf("parse sprint issues: %w", err)
	}
	return resp, nil
}

// ListBoardIssues fetches issues on a board via the Agile API.
// GET /board/{id}/issue?startAt=&maxResults=&fields=
func (c *Client) ListBoardIssues(ctx context.Context, boardID, page, limit int, fields []string) (SearchResponse, error) {
	q := url.Values{}
	q.Set("startAt", fmt.Sprintf("%d", (page-1)*limit))
	q.Set("maxResults", fmt.Sprintf("%d", limit))
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	body, status, err := c.AgileGet(ctx, fmt.Sprintf("/board/%d/issue", boardID), q)
	if err != nil {
		return SearchResponse{}, err
	}
	if status != 200 {
		return SearchResponse{}, fmt.Errorf("board issues: %w", MapStatus("", status, body))
	}
	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return SearchResponse{}, fmt.Errorf("parse board issues: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Sprint membership mutations
// ---------------------------------------------------------------------------

// MoveIssuesToSprint moves one or more issues into a sprint.
// POST /sprint/{id}/issue  with {"issues":[...]}
// Cache invalidation: callers must delete sprints/* and issue-summary/<KEY> after success.
func (c *Client) MoveIssuesToSprint(ctx context.Context, sprintID int, keys []string) error {
	payload := map[string]any{"issues": keys}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal move-to-sprint: %w", err)
	}
	body, status, err := c.AgilePost(ctx, fmt.Sprintf("/sprint/%d/issue", sprintID), nil, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	if status == 204 || status == 200 || status == 201 {
		return nil
	}
	// Check for partial failure: {"errors":{"KEY":"reason"}}
	var partial struct {
		Errors map[string]string `json:"errors"`
	}
	if jsonErr := json.Unmarshal(body, &partial); jsonErr == nil && len(partial.Errors) > 0 {
		msgs := make([]string, 0, len(partial.Errors))
		for k, v := range partial.Errors {
			msgs = append(msgs, k+": "+v)
		}
		return fmt.Errorf("partial failure: %s", strings.Join(msgs, "; "))
	}
	return fmt.Errorf("move to sprint: %w", MapStatus("", status, body))
}

// MoveIssuesToBacklog moves one or more issues to the backlog (removes from any sprint).
// POST /backlog/issue  with {"issues":[...]}
func (c *Client) MoveIssuesToBacklog(ctx context.Context, keys []string) error {
	payload := map[string]any{"issues": keys}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal move-to-backlog: %w", err)
	}
	body, status, err := c.AgilePost(ctx, "/backlog/issue", nil, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	if status == 204 || status == 200 || status == 201 {
		return nil
	}
	if status == 404 {
		return fmt.Errorf("backlog endpoint requires --board <scrum-board-id> on this Jira version: %w", ErrNotFound)
	}
	return fmt.Errorf("move to backlog: %w", MapStatus("", status, body))
}

// ---------------------------------------------------------------------------
// Sprint field discovery
// ---------------------------------------------------------------------------

// ResolveSprintField returns the custom-field ID for the Sprint field.
// Tier 1: Name=="Sprint" AND Schema.Custom=="com.pyxis.greenhopper.jira:gh-sprint".
// Tier 2: Name=="Sprint" AND Schema.Type=="array" (fallback when custom type absent).
// Returns "" when no match found — never errors on missing field.
func (c *Client) ResolveSprintField(ctx context.Context, store *cache.Store, noCache bool) (string, error) {
	fields, err := c.ListFields(ctx, store, noCache)
	if err != nil {
		return "", err
	}
	// Tier 1: exact schema match
	for _, f := range fields {
		if f.Name == "Sprint" && f.Schema != nil && f.Schema.Custom == "com.pyxis.greenhopper.jira:gh-sprint" {
			return f.ID, nil
		}
	}
	// Tier 2: name + array type fallback
	for _, f := range fields {
		if f.Name == "Sprint" && f.Schema != nil && f.Schema.Type == "array" {
			return f.ID, nil
		}
	}
	return "", nil
}
