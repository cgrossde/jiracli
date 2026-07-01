package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/keychain"
)

func TestParseSprintTime(t *testing.T) {
	cases := map[string]bool{ // input -> expect zero
		"":                              true,
		"not-a-date":                    true,
		"2026-01-02":                    false,
		"2026-01-02T15:04:05.000Z":      false,
		"2026-01-02T15:04:05Z":          false,
		"2026-01-02T15:04:05.999+02:00": false,
	}
	for in, wantZero := range cases {
		got := parseSprintTime(in)
		if got.IsZero() != wantZero {
			t.Errorf("parseSprintTime(%q).IsZero() = %v, want %v", in, got.IsZero(), wantZero)
		}
	}
}

func TestIsImmutableClosed(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	recent := now.Add(-10 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")

	if !isImmutableClosed(Sprint{State: "closed", EndDate: old}, now) {
		t.Error("closed sprint ended 100d ago should be immutable")
	}
	if isImmutableClosed(Sprint{State: "closed", EndDate: recent}, now) {
		t.Error("closed sprint ended 10d ago should NOT be immutable")
	}
	if isImmutableClosed(Sprint{State: "active", EndDate: old}, now) {
		t.Error("active sprint should never be immutable")
	}
	if isImmutableClosed(Sprint{State: "closed", EndDate: ""}, now) {
		t.Error("closed sprint without endDate should not be immutable")
	}
}

// sprintTestServer builds an httptest server that serves the GreenHopper
// sprintquery endpoint and per-sprint GetSprint lookups. It records how many
// times each sprint id is fetched via GetSprint.
type sprintTestServer struct {
	*httptest.Server
	mu       sync.Mutex
	getCalls map[int]int
	endDates map[int]string
}

var sprintPathRe = regexp.MustCompile(`/rest/agile/1\.0/sprint/(\d+)$`)

func newSprintTestServer(t *testing.T, boardID int, sprints []Sprint) *sprintTestServer {
	t.Helper()
	s := &sprintTestServer{getCalls: map[int]int{}, endDates: map[int]string{}}
	for _, sp := range sprints {
		s.endDates[sp.ID] = sp.EndDate
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GreenHopper sprintquery: return all id/name/state (uppercase, no dates).
		if r.URL.Path == fmt.Sprintf("/rest/greenhopper/1.0/sprintquery/%d", boardID) {
			env := sprintQueryEnvelope{RapidViewID: boardID}
			for i, sp := range sprints {
				env.Sprints = append(env.Sprints, struct {
					ID       int    `json:"id"`
					Name     string `json:"name"`
					State    string `json:"state"`
					Sequence int    `json:"sequence"`
				}{ID: sp.ID, Name: sp.Name, State: upper(sp.State), Sequence: i})
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(env)
			return
		}
		// GetSprint: return the dated record and count the call.
		if m := sprintPathRe.FindStringSubmatch(r.URL.Path); m != nil {
			id, _ := strconv.Atoi(m[1])
			s.mu.Lock()
			s.getCalls[id]++
			end := s.endDates[id]
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(Sprint{ID: id, EndDate: end, StartDate: end})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(s.Server.Close)
	return s
}

func (s *sprintTestServer) calls(id int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCalls[id]
}

func upper(s string) string {
	switch s {
	case "closed":
		return "CLOSED"
	case "active":
		return "ACTIVE"
	case "future":
		return "FUTURE"
	}
	return s
}

// testStore returns a cache.Store rooted in a per-test temp dir.
func testStore(t *testing.T, url string) *cache.Store {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return cache.NewStore(keychain.Entry{Profile: "test", URL: url})
}

func idsOf(sprints []Sprint) map[int]bool {
	out := map[int]bool{}
	for _, s := range sprints {
		out[s.ID] = true
	}
	return out
}

func TestListSprintsDefaultView_recencyWindowAndEarlyStop(t *testing.T) {
	now := time.Now()
	fmtDate := func(d time.Duration) string { return now.Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }

	board := 42
	sprints := []Sprint{
		{ID: 200, Name: "S200", State: "active", EndDate: fmtDate(3 * 24 * time.Hour)},
		{ID: 199, Name: "S199", State: "future", EndDate: fmtDate(17 * 24 * time.Hour)},
		{ID: 198, Name: "S198", State: "closed", EndDate: fmtDate(-2 * 24 * time.Hour)},   // in window
		{ID: 197, Name: "S197", State: "closed", EndDate: fmtDate(-10 * 24 * time.Hour)},  // out → stop
		{ID: 196, Name: "S196", State: "closed", EndDate: fmtDate(-120 * 24 * time.Hour)}, // must not be fetched
		{ID: 195, Name: "S195", State: "closed", EndDate: fmtDate(-200 * 24 * time.Hour)}, // must not be fetched
	}
	srv := newSprintTestServer(t, board, sprints)
	client := New(keychain.Entry{URL: srv.URL, PAT: "x"})
	store := testStore(t, srv.URL)

	got, err := client.ListSprintsDefaultView(context.Background(), board, DefaultClosedWindow, store, false)
	if err != nil {
		t.Fatalf("ListSprintsDefaultView: %v", err)
	}

	ids := idsOf(got)
	for _, want := range []int{200, 199, 198} {
		if !ids[want] {
			t.Errorf("expected sprint %d in default view, got ids %v", want, ids)
		}
	}
	for _, unwanted := range []int{197, 196, 195} {
		if ids[unwanted] {
			t.Errorf("sprint %d should be outside the 7d window, got ids %v", unwanted, ids)
		}
	}
	// The early-stop scan must never touch sprints older than the first
	// out-of-window one (197).
	if srv.calls(196) != 0 || srv.calls(195) != 0 {
		t.Errorf("early-stop failed: old closed sprints were fetched (196=%d, 195=%d)", srv.calls(196), srv.calls(195))
	}
}

func TestListAllSprintsHydrated_archiveSkipsRefetch(t *testing.T) {
	now := time.Now()
	fmtDate := func(d time.Duration) string { return now.Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }

	board := 7
	sprints := []Sprint{
		{ID: 300, Name: "S300", State: "active", EndDate: fmtDate(3 * 24 * time.Hour)},
		{ID: 298, Name: "S298", State: "closed", EndDate: fmtDate(-5 * 24 * time.Hour)},   // mutable
		{ID: 296, Name: "S296", State: "closed", EndDate: fmtDate(-120 * 24 * time.Hour)}, // immutable → archived
		{ID: 295, Name: "S295", State: "closed", EndDate: fmtDate(-400 * 24 * time.Hour)}, // immutable → archived
	}
	srv := newSprintTestServer(t, board, sprints)
	client := New(keychain.Entry{URL: srv.URL, PAT: "x"})
	store := testStore(t, srv.URL)

	first, err := client.ListAllSprintsHydrated(context.Background(), board, store, false)
	if err != nil {
		t.Fatalf("first ListAllSprintsHydrated: %v", err)
	}
	if len(first) != 4 {
		t.Fatalf("expected 4 sprints, got %d", len(first))
	}
	// First pass hydrates everything.
	if srv.calls(296) == 0 || srv.calls(295) == 0 {
		t.Fatalf("first pass should hydrate old closed sprints")
	}

	// Reset counters; second pass must serve immutable sprints from the archive.
	srv.mu.Lock()
	srv.getCalls = map[int]int{}
	srv.mu.Unlock()

	second, err := client.ListAllSprintsHydrated(context.Background(), board, store, false)
	if err != nil {
		t.Fatalf("second ListAllSprintsHydrated: %v", err)
	}
	if len(second) != 4 {
		t.Fatalf("expected 4 sprints on second pass, got %d", len(second))
	}
	if srv.calls(296) != 0 || srv.calls(295) != 0 {
		t.Errorf("archive should prevent refetch of immutable sprints (296=%d, 295=%d)", srv.calls(296), srv.calls(295))
	}
	// Dates must still be present for archived sprints.
	for _, s := range second {
		if s.ID == 296 && s.EndDate == "" {
			t.Error("archived sprint 296 lost its endDate on the second pass")
		}
	}
}
