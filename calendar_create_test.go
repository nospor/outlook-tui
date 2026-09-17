package main

import (
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
