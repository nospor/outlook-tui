package main

import (
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
