package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEscapeODataString(t *testing.T) {
	if got := escapeODataString("it's"); got != "it''s" {
		t.Errorf("escapeODataString = %q, want it''s", got)
	}
}

func TestMessageMatchesConversation(t *testing.T) {
	msg := Message{ID: "solo-id", ConversationID: ""}
	if !messageMatchesConversation(msg, "solo-id") {
		t.Error("empty conversationId should match message id")
	}
	msg2 := Message{ID: "msg-1", ConversationID: "conv-1"}
	if !messageMatchesConversation(msg2, "conv-1") {
		t.Error("expected conversation match")
	}
	if messageMatchesConversation(msg2, "conv-2") {
		t.Error("expected no match")
	}
}

func TestGetConversationMessageIDs(t *testing.T) {
	conversationID := "conv-123"
	folderID := "inbox-folder"
	requestCount := 0
	var nextPageURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/mailFolders/"+folderID+"/messages") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		requestCount++
		hasFilter := strings.Contains(r.URL.RawQuery, "filter")
		var value []Message
		if hasFilter {
			if requestCount == 1 {
				value = []Message{
					{ID: "hit-1", ConversationID: conversationID},
					{ID: "hit-2", ConversationID: conversationID},
				}
			} else {
				value = []Message{{ID: "hit-3", ConversationID: conversationID}}
			}
		} else {
			if requestCount == 1 {
				value = []Message{
					{ID: "other-1", ConversationID: "conv-other"},
					{ID: "hit-1", ConversationID: conversationID},
					{ID: "hit-2", ConversationID: conversationID},
				}
			} else {
				value = []Message{{ID: "hit-3", ConversationID: conversationID}}
			}
		}
		payload := map[string]interface{}{"value": value}
		if requestCount == 1 {
			payload["@odata.nextLink"] = nextPageURL
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()
	nextPageURL = server.URL + "/me/mailFolders/" + folderID + "/messages?page=2"

	oldBase := graphBaseURL
	graphBaseURL = server.URL
	t.Cleanup(func() { graphBaseURL = oldBase })

	gc := NewGraphClient(server.Client())
	got, err := gc.GetConversationMessageIDs(folderID, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d ids, want 3: %v", len(got), got)
	}
	if requestCount != 2 {
		t.Errorf("expected 2 paginated requests, got %d", requestCount)
	}
}

func TestDeleteConversationMessagesWaves(t *testing.T) {
	conversationID := "conv-wave"
	folderID := "inbox-folder"
	remaining := map[string]bool{"a": true, "b": true, "c": true}
	deleteCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/messages"):
			var hits []Message
			for id := range remaining {
				hits = append(hits, Message{ID: id, ConversationID: conversationID})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"value": hits})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/move"):
			deleteCalls++
			parts := strings.Split(r.URL.Path, "/")
			id := parts[len(parts)-2]
			delete(remaining, id)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	oldBase := graphBaseURL
	graphBaseURL = server.URL
	t.Cleanup(func() { graphBaseURL = oldBase })

	gc := NewGraphClient(server.Client())
	succeeded, failed, errs := gc.DeleteConversationMessages(folderID, conversationID, nil, false)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(failed) > 0 {
		t.Fatalf("unexpected failed ids: %v", failed)
	}
	if len(succeeded) != 3 {
		t.Fatalf("expected 3 succeeded, got %d: %v", len(succeeded), succeeded)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected all messages removed, remaining: %v", remaining)
	}
	if deleteCalls != 3 {
		t.Errorf("expected 3 delete calls, got %d", deleteCalls)
	}
}

func TestParseAttendeeList(t *testing.T) {
	tests := []struct {
		input    string
		wantLen  int
		wantType string
		wantAddr string
	}{
		{"alice@example.com", 1, "required", "alice@example.com"},
		{"?bob@example.com", 1, "optional", "bob@example.com"},
		{"!room@example.com", 1, "resource", "room@example.com"},
		{"Alice <alice@example.com>, ?Bob <bob@example.com>", 2, "required", "alice@example.com"},
	}
	for _, tc := range tests {
		got := ParseAttendeeList(tc.input)
		if len(got) != tc.wantLen {
			t.Errorf("ParseAttendeeList(%q) len = %d, want %d", tc.input, len(got), tc.wantLen)
			continue
		}
		if got[0].Type != tc.wantType {
			t.Errorf("ParseAttendeeList(%q) type = %q, want %q", tc.input, got[0].Type, tc.wantType)
		}
		if got[0].Address != tc.wantAddr {
			t.Errorf("ParseAttendeeList(%q) addr = %q, want %q", tc.input, got[0].Address, tc.wantAddr)
		}
	}
}

func TestBuildRecurrencePattern(t *testing.T) {
	tests := []struct {
		name string
		r    RecurrenceSettings
		want string // pattern type key
	}{
		{"disabled", RecurrenceSettings{Enabled: false}, ""},
		{"daily", RecurrenceSettings{Enabled: true, PatternType: "daily", Interval: 1, RangeType: "noEnd", StartDate: "2026-01-01"}, "daily"},
		{"weekly", RecurrenceSettings{Enabled: true, PatternType: "weekly", Interval: 2, DaysOfWeek: []string{"monday", "wednesday"}, RangeType: "endDate", StartDate: "2026-01-01", EndDate: "2026-12-31"}, "weekly"},
		{"absoluteMonthly", RecurrenceSettings{Enabled: true, PatternType: "absoluteMonthly", DayOfMonth: 15, RangeType: "numbered", StartDate: "2026-01-01", NumberedCount: 10}, "absoluteMonthly"},
		{"relativeMonthly", RecurrenceSettings{Enabled: true, PatternType: "relativeMonthly", Index: "second", DayOfWeek: "tuesday", RangeType: "noEnd", StartDate: "2026-01-01"}, "relativeMonthly"},
		{"absoluteYearly", RecurrenceSettings{Enabled: true, PatternType: "absoluteYearly", DayOfMonth: 4, Interval: 7, RangeType: "noEnd", StartDate: "2026-01-01"}, "absoluteYearly"},
		{"relativeYearly", RecurrenceSettings{Enabled: true, PatternType: "relativeYearly", Index: "last", DayOfWeek: "friday", DayOfMonth: 12, RangeType: "noEnd", StartDate: "2026-01-01"}, "relativeYearly"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildRecurrencePattern(tc.r)
			if tc.want == "" {
				if got != nil {
					t.Errorf("expected nil recurrence")
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil recurrence")
			}
			pat, ok := got["pattern"].(map[string]interface{})
			if !ok {
				t.Fatal("missing pattern")
			}
			if pat["type"] != tc.want {
				t.Errorf("pattern type = %v, want %q", pat["type"], tc.want)
			}
		})
	}
}

func TestCountScheduleConflicts(t *testing.T) {
	queryStart := time.Date(2026, 1, 15, 8, 0, 0, 0, time.Local)
	eventStart := time.Date(2026, 1, 15, 9, 0, 0, 0, time.Local)
	eventEnd := time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local)
	schedules := []ScheduleInformation{
		{ScheduleID: "a@x.com", AvailabilityView: "0022000000"},
		{ScheduleID: "b@x.com", AvailabilityView: "0000000000"},
	}
	conflicts := CountScheduleConflicts(schedules, queryStart, eventStart, eventEnd, 30)
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", conflicts)
	}
}

func TestAvailabilitySymbol(t *testing.T) {
	cases := map[byte]string{
		'0': ".",
		'1': "~",
		'2': "#",
		'3': "!",
		'4': "W",
	}
	for code, want := range cases {
		if got := AvailabilitySymbol(code); got != want {
			t.Errorf("AvailabilitySymbol(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestParseEventDateTime(t *testing.T) {
	tm, err := ParseEventDateTime("2026-03-15 14:30")
	if err != nil {
		t.Fatal(err)
	}
	if tm.Year() != 2026 || tm.Month() != 3 || tm.Day() != 15 || tm.Hour() != 14 || tm.Minute() != 30 {
		t.Errorf("unexpected time: %v", tm)
	}
	_, err = ParseEventDateTime("bad")
	if err == nil {
		t.Error("expected error for bad input")
	}
}

func TestCreateEventRequestJSON(t *testing.T) {
	start := time.Date(2026, 3, 15, 10, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	rec := BuildRecurrencePattern(RecurrenceSettings{
		Enabled: true, PatternType: "weekly", DaysOfWeek: []string{"monday"},
		RangeType: "noEnd", StartDate: "2026-03-15",
	})
	body := map[string]interface{}{
		"subject":    "Team sync",
		"start":      toGraphDateTime(start),
		"end":        toGraphDateTime(end),
		"showAs":     "busy",
		"recurrence": rec,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Error("invalid JSON")
	}
}

func TestIsValidAttendeeEmail(t *testing.T) {
	if !IsValidAttendeeEmail("alice@example.com") {
		t.Error("expected valid")
	}
	if IsValidAttendeeEmail("r") {
		t.Error("expected invalid partial")
	}
	if IsValidAttendeeEmail("bob@") {
		t.Error("expected invalid incomplete")
	}
}

func TestFilterValidAttendees(t *testing.T) {
	got := FilterValidAttendees(ParseAttendeeList("alice@example.com, bob, charlie@test.org"))
	if len(got) != 2 {
		t.Fatalf("got %d valid attendees, want 2", len(got))
	}
}

func TestIanaFromZoneinfoPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/share/zoneinfo/Europe/London", "Europe/London"},
		{"../usr/share/zoneinfo/America/New_York", "America/New_York"},
		{"/etc/localtime", ""},
	}
	for _, tc := range tests {
		if got := ianaFromZoneinfoPath(tc.path); got != tc.want {
			t.Errorf("ianaFromZoneinfoPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestFormatMeetingConfidence(t *testing.T) {
	if got := formatMeetingConfidence(100); got != "100%" {
		t.Errorf("got %q, want 100%%", got)
	}
	if got := formatMeetingConfidence(1.0); got != "100%" {
		t.Errorf("got %q, want 100%%", got)
	}
	if got := formatMeetingConfidence(0.85); got != "85%" {
		t.Errorf("got %q, want 85%%", got)
	}
}

func TestParseAttendeeField(t *testing.T) {
	got := ParseAttendeeField("alice@example.com, ?bob@example.com", "required")
	if len(got) != 2 || got[0].Type != "required" || got[1].Type != "required" {
		t.Fatalf("required field types: %+v", got)
	}
	got = ParseAttendeeField("bob@example.com, !room@example.com", "optional")
	if len(got) != 2 || got[0].Type != "optional" || got[1].Type != "resource" {
		t.Fatalf("optional field types: %+v", got)
	}
}

func TestLocalTimeZoneNotAbbreviation(t *testing.T) {
	tz := localTimeZone()
	if tz == "BST" || tz == "CET" || tz == "EST" || tz == "PST" {
		t.Errorf("localTimeZone() returned abbreviation %q, Graph requires IANA or Windows name", tz)
	}
}

func TestRecurrencePreview(t *testing.T) {
	p := RecurrencePreview(RecurrenceSettings{
		Enabled: true, PatternType: "weekly", Interval: 2,
		DaysOfWeek: []string{"monday", "wednesday"}, RangeType: "endDate", EndDate: "2026-12-31",
	})
	if p == "" || p == "None" {
		t.Errorf("unexpected preview: %q", p)
	}
}

func TestCalendarDateTimeGMTStandardTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("Europe/London unavailable")
	}
	want := time.Date(2026, 8, 25, 10, 0, 0, 0, loc)
	got := CalendarDateTime{
		DateTime: "2026-08-25T10:00:00",
		TimeZone: "GMT Standard Time",
	}.Time().In(loc)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFilterAndSortMeetingSuggestions(t *testing.T) {
	loc := time.Local
	tue := time.Date(2026, 8, 25, 0, 0, 0, 0, loc)
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, loc)
	suggestions := []MeetingTimeSuggestion{
		{Start: time.Date(2026, 8, 24, 14, 0, 0, 0, loc)},
		{Start: time.Date(2026, 8, 25, 14, 0, 0, 0, loc)},
		{Start: time.Date(2026, 8, 24, 16, 0, 0, 0, loc)},
		{Start: time.Date(2026, 8, 26, 10, 0, 0, 0, loc)},
		{Start: time.Date(2026, 8, 25, 11, 0, 0, 0, loc)},
	}
	out := filterAndSortMeetingSuggestions(suggestions, tue, now)
	if len(out) != 3 {
		t.Fatalf("expected 3 suggestions, got %d", len(out))
	}
	if out[0].Start.Day() != 25 || out[0].Start.Hour() != 11 {
		t.Errorf("expected Tuesday 11:00 first, got %v", out[0].Start)
	}
	if out[1].Start.Day() != 25 || out[1].Start.Hour() != 14 {
		t.Errorf("expected Tuesday 14:00 second, got %v", out[1].Start)
	}
}
