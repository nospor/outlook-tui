package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSyncEventCreateEndFromStart(t *testing.T) {
	start := time.Date(2026, 9, 15, 14, 0, 0, 0, time.Local)
	m := &mainModel{}
	m.eventCreateStart = NewDateTimePicker(start)
	m.eventCreateEnd = NewDateTimePicker(start.Add(time.Hour))

	m.syncEventCreateEndFromStart()

	want := start.Add(30 * time.Minute)
	got := m.eventCreateEnd.Time()
	if !got.Equal(want) {
		t.Fatalf("end = %v, want %v", got, want)
	}
}

func TestInitEventEditFormFetchesAvailability(t *testing.T) {
	start := time.Date(2026, 9, 17, 14, 0, 0, 0, time.UTC)
	ev := CalendarEvent{
		ID:      "evt-1",
		Subject: "Standup",
		Start:   CalendarDateTime{DateTime: "2026-09-17T14:00:00", TimeZone: "UTC"},
		End:     CalendarDateTime{DateTime: "2026-09-17T15:00:00", TimeZone: "UTC"},
		Attendees: []CalendarEventAttendee{
			{EmailAddress: EmailAddress{Address: "a@example.com"}, Type: "required"},
		},
	}
	m := &mainModel{graphClient: NewGraphClient(&http.Client{})}
	cmd := m.initEventEditForm(ev)
	if cmd == nil {
		t.Fatal("edit form should fetch availability immediately when times and attendees are known")
	}
	if !m.eventCreateAvailLoading {
		t.Fatal("expected availability loading after opening edit form")
	}
	if !m.eventCreateIsEditing() {
		t.Fatal("expected edit mode")
	}
	gotStart, gotEnd, err := m.parsedEventCreateTimes()
	if err != nil {
		t.Fatal(err)
	}
	if !gotStart.Equal(start.Local()) {
		t.Fatalf("start = %v, want %v", gotStart, start.Local())
	}
	if !gotEnd.Equal(start.Add(time.Hour).Local()) {
		t.Fatalf("end = %v, want %v", gotEnd, start.Add(time.Hour).Local())
	}
}

func TestInitEventCreateFormDoesNotFetchAvailability(t *testing.T) {
	m := &mainModel{graphClient: NewGraphClient(&http.Client{})}
	m.initEventCreateForm(time.Time{})
	if m.eventCreateAvailLoading {
		t.Fatal("new event form should not fetch availability before attendees are set")
	}
	if len(m.eventCreateSchedules) != 0 {
		t.Fatal("new event form should start with empty availability")
	}
}

func TestFormatAvailabilityLocalClocks(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip("Europe/London unavailable")
	}
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("America/Chicago unavailable")
	}
	event := time.Date(2026, 9, 18, 16, 30, 0, 0, london)
	if event.In(chicago).Format("2006-01-02 15:04") == event.In(time.Local).Format("2006-01-02 15:04") {
		t.Skip("organizer local clock matches US Central")
	}
	schedules := []ScheduleInformation{
		{
			ScheduleID: "benito@example.com",
			WorkingHours: &ScheduleWorkingHours{
				TimeZone: &ScheduleTimeZone{Name: "Central Standard Time"},
			},
		},
	}
	got := formatAvailabilityLocalClocks(schedules, event)
	if !strings.Contains(got, "benito") || !strings.Contains(got, "10:30") {
		t.Fatalf("got %q, want attendee local 10:30", got)
	}
}
