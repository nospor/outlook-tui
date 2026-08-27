package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	eventCreateStepSubject = iota
	eventCreateStepAttendees
	eventCreateStepStart
	eventCreateStepEnd
	eventCreateStepLocation
	eventCreateStepBody
	eventCreateStepOptions
	eventCreateStepCount
)

const (
	eventCreateAttendeeRequired = iota
	eventCreateAttendeeOptional
)

const (
	eventCreateOptAllDay = iota
	eventCreateOptTeams
	eventCreateOptShowAs
	eventCreateOptReminderOn
	eventCreateOptReminderMin
	eventCreateOptRecurrence
	eventCreateOptCount
)

type recurFieldKind int

const (
	rfPattern recurFieldKind = iota
	rfInterval
	rfWeeklyDays
	rfDayOfMonth
	rfIndex
	rfDayOfWeek
	rfRange
	rfEndDate
	rfOccurrences
)

const eventCreateAvailIntervalMin = 30
const eventCreateSlotCharWidth = 4 // visual width per 30-min slot in busy timeline

// Calendar create tea messages
type (
	scheduleFetchedMsg struct {
		Schedules          []ScheduleInformation
		ScheduleQueryStart time.Time
	}
	meetingTimesFetchedMsg struct {
		Result *MeetingTimeSuggestionsResult
	}
	eventCreatedMsg struct {
		Event *CalendarEvent
	}
	eventUpdatedMsg struct {
		Event *CalendarEvent
	}
	calendarEventLoadedForEditMsg struct {
		Event *CalendarEvent
	}
	eventCreateEditorLoadedMsg string
	eventCreateAvailDebounceMsg struct{}
)

func (m *mainModel) initEventCreateForm(prefillDay time.Time) {
	w := m.width - 24
	if w < 30 {
		w = 30
	}

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
	if !prefillDay.IsZero() {
		start = time.Date(prefillDay.Year(), prefillDay.Month(), prefillDay.Day(), 9, 0, 0, 0, prefillDay.Location())
	}
	dur := time.Duration(m.config.CalendarDefaultDurationMin) * time.Minute
	if dur <= 0 {
		dur = time.Hour
	}
	end := start.Add(dur)

	m.eventCreateSubject = textinput.New()
	m.eventCreateSubject.Placeholder = "Meeting subject..."
	m.eventCreateSubject.Width = w
	m.eventCreateSubject.Focus()

	m.eventCreateAttendees = textinput.New()
	m.eventCreateAttendees.Placeholder = "email@domain.com, name <email@domain.com>"
	m.eventCreateAttendees.Width = w

	m.eventCreateOptionalAttendees = textinput.New()
	m.eventCreateOptionalAttendees.Placeholder = "optional@domain.com (optional)"
	m.eventCreateOptionalAttendees.Width = w

	m.eventCreateStart = NewDateTimePicker(start)
	m.eventCreateEnd = NewDateTimePicker(end)

	m.eventCreateLocation = textinput.New()
	m.eventCreateLocation.Placeholder = "Room or location (optional)"
	m.eventCreateLocation.Width = w

	m.eventCreateBody = textarea.New()
	m.eventCreateBody.ShowLineNumbers = false
	m.eventCreateBody.Placeholder = "Event description..."
	m.eventCreateBody.SetWidth(w)
	m.eventCreateBody.SetHeight(6)

	inputW := w
	if inputW > 24 {
		inputW = 24
	}
	m.eventCreateReminderMinInput = textinput.New()
	m.eventCreateReminderMinInput.Placeholder = "15"
	m.eventCreateReminderMinInput.Width = inputW
	m.eventCreateReminderMinInput.SetValue(fmt.Sprintf("%d", m.config.CalendarDefaultReminderMin))

	m.eventCreateRecurrenceIntervalInput = textinput.New()
	m.eventCreateRecurrenceIntervalInput.Placeholder = "1"
	m.eventCreateRecurrenceIntervalInput.Width = inputW
	m.eventCreateRecurrenceIntervalInput.SetValue("1")

	m.eventCreateRecurrenceDaysInput = textinput.New()
	m.eventCreateRecurrenceDaysInput.Placeholder = "mon,wed,fri"
	m.eventCreateRecurrenceDaysInput.Width = w

	m.eventCreateRecurrenceDayInput = textinput.New()
	m.eventCreateRecurrenceDayInput.Placeholder = "1-31"
	m.eventCreateRecurrenceDayInput.Width = inputW

	m.eventCreateRecurrenceEndDateInput = textinput.New()
	m.eventCreateRecurrenceEndDateInput.Placeholder = "YYYY-MM-DD"
	m.eventCreateRecurrenceEndDateInput.Width = w

	m.eventCreateRecurrenceCountInput = textinput.New()
	m.eventCreateRecurrenceCountInput.Placeholder = "10"
	m.eventCreateRecurrenceCountInput.Width = inputW
	m.eventCreateRecurrenceCountInput.SetValue("10")

	m.eventCreateStep = eventCreateStepSubject
	m.eventCreateAllDay = false
	m.eventCreateTeams = true
	m.eventCreateReminderOn = true
	m.eventCreateShowAs = "busy"
	m.eventCreateReminderMin = m.config.CalendarDefaultReminderMin
	if m.eventCreateReminderMin <= 0 {
		m.eventCreateReminderMin = 15
	}
	m.eventCreateReminderMinInput.SetValue(fmt.Sprintf("%d", m.eventCreateReminderMin))
	m.eventCreateRecurrence = RecurrenceSettings{}
	m.eventCreateRecurrenceEnabled = false
	m.eventCreateSchedules = nil
	m.eventCreateSuggestions = nil
	m.eventCreateSuggestionsSelected = 0
	m.eventCreateAvailLoading = false
	m.eventCreateConflictCount = 0
	m.eventCreateScheduleQueryStart = time.Time{}
	m.eventCreateAvailDebounce = nil
	m.eventCreateFocusSuggestions = false
	m.eventCreateAttendeesStep = 0
	m.eventCreateOptionsStep = 0
	m.eventCreateRecurrenceStep = 0

	m.eventCreateEditingID = ""
	m.loadContacts()
	m.loadAttendeeLists()
	m.updateEventCreateFocus()
}

func formatCalendarAttendeesField(attendees []CalendarEventAttendee, wantType string) string {
	var parts []string
	for _, a := range attendees {
		typ := a.Type
		if typ == "" {
			typ = "required"
		}
		if typ != wantType {
			continue
		}
		addr := strings.TrimSpace(a.EmailAddress.Address)
		if addr == "" {
			continue
		}
		name := strings.TrimSpace(a.EmailAddress.Name)
		if name != "" && !strings.EqualFold(name, addr) {
			parts = append(parts, fmt.Sprintf("%s <%s>", name, addr))
		} else {
			parts = append(parts, addr)
		}
	}
	return strings.Join(parts, ", ")
}

func calendarEventBodyText(ev CalendarEvent) string {
	if content := strings.TrimSpace(ev.Body.Content); content != "" {
		if ct := strings.ToLower(strings.TrimSpace(ev.Body.ContentType)); ct == "text" || ct == "" {
			return content
		}
	}
	return strings.TrimSpace(ev.BodyPreview)
}

func (m *mainModel) initEventEditForm(ev CalendarEvent) {
	m.initEventCreateForm(ev.Start.Time().Local())
	m.eventCreateEditingID = ev.ID

	m.eventCreateSubject.SetValue(ev.Subject)
	m.eventCreateAttendees.SetValue(formatCalendarAttendeesField(ev.Attendees, "required"))
	m.eventCreateOptionalAttendees.SetValue(formatCalendarAttendeesField(ev.Attendees, "optional"))
	m.eventCreateStart.SetTime(ev.Start.Time().Local())
	m.eventCreateEnd.SetTime(ev.End.Time().Local())
	m.eventCreateLocation.SetValue(ev.Location.DisplayName)
	m.eventCreateBody.SetValue(calendarEventBodyText(ev))

	m.eventCreateAllDay = ev.IsAllDay
	m.eventCreateTeams = ev.IsOnlineMeeting
	m.eventCreateReminderOn = ev.IsReminderOn
	if ev.ShowAs != "" {
		m.eventCreateShowAs = ev.ShowAs
	}
	reminderMin := ev.ReminderMinutesBeforeStart
	if reminderMin <= 0 {
		reminderMin = m.config.CalendarDefaultReminderMin
	}
	if reminderMin <= 0 {
		reminderMin = 15
	}
	m.eventCreateReminderMin = reminderMin
	m.eventCreateReminderMinInput.SetValue(strconv.Itoa(reminderMin))

	m.eventCreateStep = eventCreateStepSubject
	m.eventCreateAttendeesStep = 0
	m.eventCreateOptionsStep = 0
	m.updateEventCreateFocus()
}

func (m mainModel) eventCreateIsEditing() bool {
	return m.eventCreateEditingID != ""
}

func (m *mainModel) syncRecurrenceInputsFromSettings() {
	r := m.eventCreateRecurrence
	interval := r.Interval
	if interval <= 0 {
		interval = 1
	}
	m.eventCreateRecurrenceIntervalInput.SetValue(strconv.Itoa(interval))
	m.eventCreateRecurrenceDaysInput.SetValue(strings.Join(r.DaysOfWeek, ", "))
	day := r.DayOfMonth
	if day <= 0 {
		day = 1
	}
	m.eventCreateRecurrenceDayInput.SetValue(strconv.Itoa(day))
	m.eventCreateRecurrenceEndDateInput.SetValue(r.EndDate)
	count := r.NumberedCount
	if count <= 0 {
		count = 10
	}
	m.eventCreateRecurrenceCountInput.SetValue(strconv.Itoa(count))
}

func (m mainModel) recurrenceFromInputs() RecurrenceSettings {
	r := m.eventCreateRecurrence
	if v, err := strconv.Atoi(strings.TrimSpace(m.eventCreateRecurrenceIntervalInput.Value())); err == nil && v > 0 {
		r.Interval = v
	}
	if days := strings.TrimSpace(m.eventCreateRecurrenceDaysInput.Value()); days != "" {
		var dow []string
		for _, p := range strings.Split(days, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			if p != "" {
				dow = append(dow, p)
			}
		}
		r.DaysOfWeek = dow
	}
	if v, err := strconv.Atoi(strings.TrimSpace(m.eventCreateRecurrenceDayInput.Value())); err == nil && v >= 1 && v <= 31 {
		r.DayOfMonth = v
	}
	r.EndDate = strings.TrimSpace(m.eventCreateRecurrenceEndDateInput.Value())
	if v, err := strconv.Atoi(strings.TrimSpace(m.eventCreateRecurrenceCountInput.Value())); err == nil && v > 0 {
		r.NumberedCount = v
	}
	return r
}

func (m *mainModel) syncRecurrenceSettingsFromInputs() {
	r := m.recurrenceFromInputs()
	if r.StartDate == "" {
		if start, _, err := m.parsedEventCreateTimes(); err == nil {
			r.StartDate = start.Format("2006-01-02")
		}
	}
	m.eventCreateRecurrence = r
}

func (m mainModel) eventCreateRecurrenceForPreview() RecurrenceSettings {
	r := m.eventCreateRecurrence
	if m.state == stateCalendarRecurrence {
		r = m.recurrenceFromInputs()
	}
	r.Enabled = true
	return r
}

func (m mainModel) recurrenceVisibleFields() []recurFieldKind {
	r := m.eventCreateRecurrence
	pt := r.PatternType
	if pt == "" {
		pt = "weekly"
	}
	fields := []recurFieldKind{rfPattern, rfInterval}
	switch pt {
	case "weekly":
		fields = append(fields, rfWeeklyDays)
	case "absoluteMonthly", "absoluteYearly":
		fields = append(fields, rfDayOfMonth)
	case "relativeMonthly", "relativeYearly":
		fields = append(fields, rfIndex, rfDayOfWeek)
	}
	fields = append(fields, rfRange)
	rt := r.RangeType
	if rt == "" {
		rt = "noEnd"
	}
	switch rt {
	case "endDate":
		fields = append(fields, rfEndDate)
	case "numbered":
		fields = append(fields, rfOccurrences)
	}
	return fields
}

func (m *mainModel) openEventCreateRecurrence() {
	if m.eventCreateRecurrence.PatternType == "" {
		m.eventCreateRecurrence.PatternType = "weekly"
		m.eventCreateRecurrence.RangeType = "noEnd"
	}
	m.syncRecurrenceInputsFromSettings()
	m.eventCreateRecurrenceStep = 0
	m.state = stateCalendarRecurrence
	m.updateEventCreateRecurrenceFocus()
}

func (m *mainModel) updateEventCreateOptionsFocus() {
	m.eventCreateReminderMinInput.Blur()
	switch m.eventCreateOptionsStep {
	case eventCreateOptReminderMin:
		m.eventCreateReminderMinInput.Focus()
	}
}

func (m *mainModel) updateEventCreateRecurrenceFocus() {
	m.eventCreateRecurrenceIntervalInput.Blur()
	m.eventCreateRecurrenceDaysInput.Blur()
	m.eventCreateRecurrenceDayInput.Blur()
	m.eventCreateRecurrenceEndDateInput.Blur()
	m.eventCreateRecurrenceCountInput.Blur()
	fields := m.recurrenceVisibleFields()
	if m.eventCreateRecurrenceStep >= len(fields) {
		m.eventCreateRecurrenceStep = 0
	}
	if len(fields) == 0 {
		return
	}
	switch fields[m.eventCreateRecurrenceStep] {
	case rfInterval:
		m.eventCreateRecurrenceIntervalInput.Focus()
	case rfWeeklyDays:
		m.eventCreateRecurrenceDaysInput.Focus()
	case rfDayOfMonth:
		m.eventCreateRecurrenceDayInput.Focus()
	case rfEndDate:
		m.eventCreateRecurrenceEndDateInput.Focus()
	case rfOccurrences:
		m.eventCreateRecurrenceCountInput.Focus()
	}
}

func (m *mainModel) updateEventCreateAttendeesFocus() {
	m.eventCreateAttendees.Blur()
	m.eventCreateOptionalAttendees.Blur()
	switch m.eventCreateAttendeesStep {
	case eventCreateAttendeeOptional:
		m.eventCreateOptionalAttendees.Focus()
	default:
		m.eventCreateAttendeesStep = eventCreateAttendeeRequired
		m.eventCreateAttendees.Focus()
	}
}

func (m *mainModel) updateEventCreateFocus() {
	m.eventCreateSubject.Blur()
	m.eventCreateAttendees.Blur()
	m.eventCreateOptionalAttendees.Blur()
	m.eventCreateStart.Blur()
	m.eventCreateEnd.Blur()
	m.eventCreateLocation.Blur()
	m.eventCreateBody.Blur()
	switch m.eventCreateStep {
	case eventCreateStepSubject:
		m.eventCreateSubject.Focus()
	case eventCreateStepAttendees:
		m.updateEventCreateAttendeesFocus()
	case eventCreateStepStart:
		m.eventCreateStart.Focus()
	case eventCreateStepEnd:
		m.eventCreateEnd.Focus()
	case eventCreateStepLocation:
		m.eventCreateLocation.Focus()
	case eventCreateStepBody:
		m.eventCreateBody.Focus()
	}
}

func (m *mainModel) updateEventCreateFilteredSuggestions() {
	if m.eventCreateStep != eventCreateStepAttendees {
		m.clearAttendeeSuggestions()
		return
	}

	val := m.activeEventCreateAttendeeInput().Value()
	query := m.attendeeSuggestionQueryFromInput(val)
	if query == "" {
		m.clearAttendeeSuggestions()
		return
	}

	var contacts []Contact
	if m.config.UseSQLite != 0 && len(m.contacts) > 0 {
		contacts = m.contacts
	}
	var lists []AttendeeList
	if m.db != nil && len(m.attendeeLists) > 0 {
		lists = m.attendeeLists
	}
	if len(contacts) == 0 && len(lists) == 0 {
		m.clearAttendeeSuggestions()
		return
	}

	m.attendeeSuggestions = filterAttendeeSuggestions(query, contacts, lists, true)
	if m.attendeeSuggestionsSelected >= len(m.attendeeSuggestions) {
		m.attendeeSuggestionsSelected = 0
		m.contactsStartIdx = 0
	}
}

func (m *mainModel) activeEventCreateAttendeeInput() *textinput.Model {
	if m.eventCreateAttendeesStep == eventCreateAttendeeOptional {
		return &m.eventCreateOptionalAttendees
	}
	return &m.eventCreateAttendees
}

func (m mainModel) eventCreateParsedAttendees() []ParsedAttendee {
	var all []ParsedAttendee
	all = append(all, ParseAttendeeField(m.eventCreateAttendees.Value(), "required")...)
	all = append(all, ParseAttendeeField(m.eventCreateOptionalAttendees.Value(), "optional")...)
	return FilterValidAttendees(all)
}

func (m mainModel) eventCreateHasContent() bool {
	return strings.TrimSpace(m.eventCreateSubject.Value()) != "" ||
		strings.TrimSpace(m.eventCreateAttendees.Value()) != "" ||
		strings.TrimSpace(m.eventCreateOptionalAttendees.Value()) != "" ||
		strings.TrimSpace(m.eventCreateBody.Value()) != ""
}

func (m mainModel) parsedEventCreateTimes() (time.Time, time.Time, error) {
	start := m.eventCreateStart.Time()
	end := m.eventCreateEnd.Time()
	if start.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start date/time")
	}
	if end.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end date/time")
	}
	return start, end, nil
}

func (m *mainModel) syncEventCreateEndFromStart() {
	start := m.eventCreateStart.Time()
	m.eventCreateEnd.SetTime(start.Add(eventCreateAvailIntervalMin * time.Minute))
}

func (m *mainModel) eventCreateOnStartTimeChanged(prev time.Time) tea.Cmd {
	if m.eventCreateStart.Time().Equal(prev) {
		return nil
	}
	m.syncEventCreateEndFromStart()
	return m.scheduleDebouncedAvailabilityRefresh()
}

func (m mainModel) eventCreateAttendeeEmails() []string {
	var emails []string
	for _, a := range m.eventCreateParsedAttendees() {
		emails = append(emails, a.Address)
	}
	return emails
}

func (m mainModel) eventCreateInTextField() bool {
	return m.eventCreateStep >= eventCreateStepSubject &&
		m.eventCreateStep <= eventCreateStepBody &&
		!m.eventCreateFocusSuggestions
}

func (m mainModel) scheduleDayBounds(day time.Time) (time.Time, time.Time) {
	startH := m.config.CalendarWorkStartHour
	endH := m.config.CalendarWorkEndHour
	if startH <= 0 {
		startH = 8
	}
	if endH <= 0 || endH <= startH {
		endH = 18
	}
	local := day.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), startH, 0, 0, 0, local.Location())
	end := time.Date(local.Year(), local.Month(), local.Day(), endH, 0, 0, 0, local.Location())
	return start, end
}

func (m mainModel) meetingTimeSearchBounds(eventDay time.Time) (time.Time, time.Time) {
	dayStart, dayEnd := m.scheduleDayBounds(eventDay)
	now := time.Now()
	searchStart := dayStart
	if now.After(searchStart) {
		searchStart = now
	}
	searchEnd := dayEnd.AddDate(0, 0, 7)
	if !searchEnd.After(searchStart) {
		searchEnd = searchStart.Add(7 * 24 * time.Hour)
	}
	return searchStart, searchEnd
}

func (m mainModel) buildCreateEventRequest() (CreateEventRequest, error) {
	start, end, err := m.parsedEventCreateTimes()
	if err != nil {
		return CreateEventRequest{}, err
	}
	if !end.After(start) {
		return CreateEventRequest{}, fmt.Errorf("end time must be after start time")
	}
	subject := strings.TrimSpace(m.eventCreateSubject.Value())
	if subject == "" {
		return CreateEventRequest{}, fmt.Errorf("subject is required")
	}
	rec := m.eventCreateRecurrence
	if m.eventCreateRecurrenceEnabled {
		rec.Enabled = true
		if rec.StartDate == "" {
			rec.StartDate = start.Format("2006-01-02")
		}
	} else {
		rec.Enabled = false
	}
	reminderMin := m.eventCreateReminderMin
	if v, err := strconv.Atoi(strings.TrimSpace(m.eventCreateReminderMinInput.Value())); err == nil && v > 0 {
		reminderMin = v
	}
	return CreateEventRequest{
		Subject:               subject,
		Body:                  m.eventCreateBody.Value(),
		Start:                 start,
		End:                   end,
		Location:              strings.TrimSpace(m.eventCreateLocation.Value()),
		Attendees:             m.eventCreateParsedAttendees(),
		IsAllDay:              m.eventCreateAllDay,
		ShowAs:                m.eventCreateShowAs,
		IsOnlineMeeting:       m.eventCreateTeams,
		IsReminderOn:          m.eventCreateReminderOn,
		ReminderMinutesBefore: reminderMin,
		Recurrence:            rec,
	}, nil
}

func fetchAttendeeAvailabilityCmd(gc *GraphClient, attendees []string, day time.Time, workStart, workEnd time.Time, eventStart, eventEnd time.Time) tea.Cmd {
	return func() tea.Msg {
		schedules, err := gc.GetAttendeeSchedule(attendees, workStart, workEnd, eventCreateAvailIntervalMin)
		if err != nil {
			return errMsg(err)
		}
		_ = eventStart
		_ = eventEnd
		return scheduleFetchedMsg{Schedules: schedules, ScheduleQueryStart: workStart}
	}
}

func findMeetingTimesCmd(gc *GraphClient, attendees []ParsedAttendee, duration time.Duration, searchStart, searchEnd time.Time) tea.Cmd {
	return func() tea.Msg {
		result, err := gc.FindMeetingTimes(FindMeetingTimesRequest{
			Attendees:   attendees,
			Duration:    duration,
			SearchStart: searchStart,
			SearchEnd:   searchEnd,
		})
		if err != nil {
			return errMsg(err)
		}
		return meetingTimesFetchedMsg{Result: result}
	}
}

func createCalendarEventCmd(gc *GraphClient, req CreateEventRequest) tea.Cmd {
	return func() tea.Msg {
		ev, err := gc.CreateCalendarEvent(req)
		if err != nil {
			return errMsg(err)
		}
		return eventCreatedMsg{Event: ev}
	}
}

func updateCalendarEventCmd(gc *GraphClient, eventID string, req CreateEventRequest) tea.Cmd {
	return func() tea.Msg {
		ev, err := gc.UpdateCalendarEvent(eventID, req)
		if err != nil {
			return errMsg(err)
		}
		return eventUpdatedMsg{Event: ev}
	}
}

func loadCalendarEventForEditCmd(gc *GraphClient, eventID string) tea.Cmd {
	return func() tea.Msg {
		ev, err := gc.GetCalendarEvent(eventID)
		if err != nil {
			return errMsg(err)
		}
		return calendarEventLoadedForEditMsg{Event: ev}
	}
}

func (m *mainModel) scheduleDebouncedAvailabilityRefresh() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
		return eventCreateAvailDebounceMsg{}
	})
}

func (m *mainModel) scheduleAvailabilityRefresh() tea.Cmd {
	if m.graphClient == nil {
		m.eventCreateAvailLoading = false
		return nil
	}
	parsed := m.eventCreateParsedAttendees()
	if len(parsed) == 0 {
		m.eventCreateAvailLoading = false
		m.eventCreateSchedules = nil
		m.eventCreateSuggestions = nil
		m.eventCreateConflictCount = 0
		return nil
	}
	attendees := make([]string, len(parsed))
	for i, a := range parsed {
		attendees[i] = a.Address
	}
	start, end, err := m.parsedEventCreateTimes()
	if err != nil {
		return nil
	}
	dayStart, dayEnd := m.scheduleDayBounds(start)
	duration := end.Sub(start)
	if duration <= 0 {
		duration = time.Hour
	}
	searchStart, searchEnd := m.meetingTimeSearchBounds(start)

	m.eventCreateAvailLoading = true
	return tea.Batch(
		fetchAttendeeAvailabilityCmd(m.graphClient, attendees, start, dayStart, dayEnd, start, end),
		findMeetingTimesCmd(m.graphClient, parsed, duration, searchStart, searchEnd),
	)
}

func (m *mainModel) applyMeetingSuggestion(idx int) {
	if idx < 0 || idx >= len(m.eventCreateSuggestions) {
		return
	}
	s := m.eventCreateSuggestions[idx]
	m.eventCreateStart.SetTime(s.Start.Local())
	m.eventCreateEnd.SetTime(s.End.Local())
}

func (m *mainModel) cycleEventCreateShowAs() {
	options := []string{"busy", "free", "tentative", "oof", "workingElsewhere"}
	for i, o := range options {
		if o == m.eventCreateShowAs {
			m.eventCreateShowAs = options[(i+1)%len(options)]
			return
		}
	}
	m.eventCreateShowAs = "busy"
}

func (m *mainModel) handleEventCreateUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case eventCreateAvailDebounceMsg:
		if m.state == stateCalendarCreate &&
			(m.eventCreateStep == eventCreateStepStart || m.eventCreateStep == eventCreateStepEnd) {
			return m, m.scheduleAvailabilityRefresh()
		}
		return m, nil

	case scheduleFetchedMsg:
		m.eventCreateAvailLoading = false
		m.eventCreateSchedules = msg.Schedules
		m.eventCreateScheduleQueryStart = msg.ScheduleQueryStart
		if start, end, err := m.parsedEventCreateTimes(); err == nil {
			m.eventCreateConflictCount = CountScheduleConflicts(
				msg.Schedules, msg.ScheduleQueryStart, start, end, eventCreateAvailIntervalMin,
			)
		}
		return m, nil

	case meetingTimesFetchedMsg:
		m.eventCreateAvailLoading = false
		if msg.Result != nil {
			preferDay := time.Now()
			if start, _, err := m.parsedEventCreateTimes(); err == nil {
				preferDay = start
			}
			m.eventCreateSuggestions = filterAndSortMeetingSuggestions(msg.Result.Suggestions, preferDay, time.Now())
			if len(m.eventCreateSuggestions) == 0 && msg.Result.EmptySuggestionsReason != "" {
				m.statusMsg = "No meeting suggestions: " + msg.Result.EmptySuggestionsReason
			}
			if m.eventCreateSuggestionsSelected >= len(m.eventCreateSuggestions) {
				m.eventCreateSuggestionsSelected = 0
			}
		}
		return m, nil

	case eventCreatedMsg:
		m.state = stateCalendar
		m.pendingCalendarSelectID = msg.Event.ID
		m.statusMsg = fmt.Sprintf("Event created: %s", msg.Event.Subject)
		cmds = append(cmds, m.loadCalendarWithCache())
		return m, tea.Batch(cmds...)

	case eventUpdatedMsg:
		m.state = stateCalendar
		m.pendingCalendarSelectID = msg.Event.ID
		m.statusMsg = fmt.Sprintf("Event updated: %s", msg.Event.Subject)
		cmds = append(cmds, m.loadCalendarWithCache())
		return m, tea.Batch(cmds...)

	case eventCreateEditorLoadedMsg:
		m.eventCreateBody.SetValue(string(msg))
		m.eventCreateStep = eventCreateStepBody
		m.updateEventCreateFocus()
		return m, nil

	case tea.KeyMsg:
		if m.state == stateCalendarCreateCancelConfirm {
			switch msg.String() {
			case "y", "Y":
				m.state = stateCalendar
				if m.eventCreateIsEditing() {
					m.statusMsg = "Event edit cancelled"
				} else {
					m.statusMsg = "Event creation cancelled"
				}
				m.eventCreateEditingID = ""
			case "n", "N", "esc":
				m.state = stateCalendarCreate
			}
			return m, nil
		}

		if m.state == stateCalendarRecurrence {
			return m.handleEventCreateRecurrenceUpdate(msg)
		}

		// Contact and attendee-list autocomplete for attendees field
		if m.eventCreateStep == eventCreateStepAttendees && len(m.attendeeSuggestions) > 0 {
			if m.handleAttendeeSuggestionKeys(msg) {
				return m, nil
			}
			if msg.String() == "enter" {
				m.applyAttendeeSuggestion(m.attendeeSuggestionsSelected, m.activeEventCreateAttendeeInput())
				return m, nil
			}
		}

		// Suggestions panel navigation
		if m.eventCreateFocusSuggestions && len(m.eventCreateSuggestions) > 0 {
			switch msg.String() {
			case "up", "k":
				if m.eventCreateSuggestionsSelected > 0 {
					m.eventCreateSuggestionsSelected--
				}
				return m, nil
			case "down", "j":
				if m.eventCreateSuggestionsSelected < len(m.eventCreateSuggestions)-1 {
					m.eventCreateSuggestionsSelected++
				}
				return m, nil
			case "enter":
				m.applyMeetingSuggestion(m.eventCreateSuggestionsSelected)
				m.eventCreateFocusSuggestions = false
				return m, m.scheduleAvailabilityRefresh()
			case "tab":
				m.eventCreateFocusSuggestions = false
				m.updateEventCreateFocus()
				return m, nil
			}
		}

		if msg.String() == "esc" {
			if m.eventCreateStep == eventCreateStepStart && m.eventCreateStart.InTextMode() {
				m.eventCreateStart.CancelTextMode()
				return m, nil
			}
			if m.eventCreateStep == eventCreateStepEnd && m.eventCreateEnd.InTextMode() {
				m.eventCreateEnd.CancelTextMode()
				return m, nil
			}
		}

		switch msg.String() {
		case "esc":
			if m.eventCreateFocusSuggestions {
				m.eventCreateFocusSuggestions = false
				m.eventCreateStep = eventCreateStepOptions
				m.updateEventCreateFocus()
				return m, nil
			}
			if m.eventCreateHasContent() || m.eventCreateIsEditing() {
				m.state = stateCalendarCreateCancelConfirm
			} else {
				m.state = stateCalendar
				m.statusMsg = "Ready"
			}
			return m, nil
		case "tab":
			if m.eventCreateFocusSuggestions {
				m.eventCreateFocusSuggestions = false
				m.eventCreateStep = eventCreateStepSubject
				m.eventCreateOptionsStep = 0
				m.updateEventCreateFocus()
				return m, nil
			}
			if m.eventCreateStep == eventCreateStepOptions {
				if m.eventCreateOptionsStep < eventCreateOptRecurrence {
					m.eventCreateOptionsStep++
					m.updateEventCreateOptionsFocus()
					return m, nil
				}
				m.eventCreateFocusSuggestions = true
				m.updateEventCreateFocus()
				return m, nil
			}
			if m.eventCreateStep == eventCreateStepAttendees {
				if m.eventCreateAttendeesStep < eventCreateAttendeeOptional {
					m.eventCreateAttendeesStep++
					m.updateEventCreateAttendeesFocus()
					return m, nil
				}
			}
			if m.eventCreateStep == eventCreateStepStart {
				prev := m.eventCreateStart.Time()
				if m.eventCreateStart.AdvanceSubFocus() {
					if cmd := m.eventCreateOnStartTimeChanged(prev); cmd != nil {
						cmds = append(cmds, cmd)
					}
					return m, tea.Batch(cmds...)
				}
			}
			if m.eventCreateStep == eventCreateStepEnd {
				prev := m.eventCreateEnd.Time()
				if m.eventCreateEnd.AdvanceSubFocus() {
					if !m.eventCreateEnd.Time().Equal(prev) {
						cmds = append(cmds, m.scheduleDebouncedAvailabilityRefresh())
					}
					return m, tea.Batch(cmds...)
				}
			}
			prevStep := m.eventCreateStep
			m.eventCreateStep = (m.eventCreateStep + 1) % eventCreateStepCount
			if m.eventCreateStep == eventCreateStepAttendees {
				m.eventCreateAttendeesStep = eventCreateAttendeeRequired
				m.updateEventCreateAttendeesFocus()
			}
			if m.eventCreateStep == eventCreateStepOptions {
				m.eventCreateOptionsStep = 0
				m.updateEventCreateOptionsFocus()
			}
			m.updateEventCreateFocus()
			if prevStep == eventCreateStepAttendees || prevStep == eventCreateStepStart || prevStep == eventCreateStepEnd {
				cmds = append(cmds, m.scheduleAvailabilityRefresh())
			}
			return m, tea.Batch(cmds...)
		case "shift+tab":
			if m.eventCreateFocusSuggestions {
				m.eventCreateFocusSuggestions = false
				m.eventCreateStep = eventCreateStepOptions
				m.eventCreateOptionsStep = eventCreateOptRecurrence
				m.updateEventCreateOptionsFocus()
				return m, nil
			}
			if m.eventCreateStep == eventCreateStepOptions {
				if m.eventCreateOptionsStep > 0 {
					m.eventCreateOptionsStep--
					m.updateEventCreateOptionsFocus()
					return m, nil
				}
				m.eventCreateStep = eventCreateStepBody
				m.updateEventCreateFocus()
				return m, nil
			}
			if m.eventCreateStep == eventCreateStepAttendees {
				if m.eventCreateAttendeesStep > eventCreateAttendeeRequired {
					m.eventCreateAttendeesStep--
					m.updateEventCreateAttendeesFocus()
					return m, nil
				}
			}
			if m.eventCreateStep == eventCreateStepStart {
				prev := m.eventCreateStart.Time()
				wasText := m.eventCreateStart.InTextMode()
				if m.eventCreateStart.RetreatSubFocus() {
					if cmd := m.eventCreateOnStartTimeChanged(prev); cmd != nil {
						cmds = append(cmds, cmd)
					}
					return m, tea.Batch(cmds...)
				}
				if wasText {
					if cmd := m.eventCreateOnStartTimeChanged(prev); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
			if m.eventCreateStep == eventCreateStepEnd {
				prev := m.eventCreateEnd.Time()
				wasText := m.eventCreateEnd.InTextMode()
				if m.eventCreateEnd.RetreatSubFocus() {
					if !m.eventCreateEnd.Time().Equal(prev) {
						cmds = append(cmds, m.scheduleDebouncedAvailabilityRefresh())
					}
					return m, tea.Batch(cmds...)
				}
				if wasText && !m.eventCreateEnd.Time().Equal(prev) {
					cmds = append(cmds, m.scheduleDebouncedAvailabilityRefresh())
				}
			}
			m.eventCreateStep = (m.eventCreateStep - 1 + eventCreateStepCount) % eventCreateStepCount
			if m.eventCreateStep == eventCreateStepAttendees {
				m.eventCreateAttendeesStep = eventCreateAttendeeOptional
				m.updateEventCreateAttendeesFocus()
			}
			if m.eventCreateStep == eventCreateStepOptions {
				m.eventCreateOptionsStep = eventCreateOptRecurrence
				m.updateEventCreateOptionsFocus()
			}
			m.updateEventCreateFocus()
			return m, nil
		case "ctrl+g":
			if m.eventCreateStep == eventCreateStepBody {
				return m, openEditorCmd(m.eventCreateBody.Value())
			}
			return m, nil
		case "ctrl+s", "ctrl+x":
			req, err := m.buildCreateEventRequest()
			if err != nil {
				m.statusMsg = err.Error()
				return m, nil
			}
			if m.eventCreateIsEditing() {
				m.statusMsg = "Updating event..."
				return m, updateCalendarEventCmd(m.graphClient, m.eventCreateEditingID, req)
			}
			m.statusMsg = "Creating event..."
			return m, createCalendarEventCmd(m.graphClient, req)
		case "R":
			if m.eventCreateStep == eventCreateStepOptions {
				m.openEventCreateRecurrence()
				return m, nil
			}
		case "r":
			if m.eventCreateStep == eventCreateStepOptions || m.eventCreateFocusSuggestions {
				return m, m.scheduleAvailabilityRefresh()
			}
		case " ":
			if m.eventCreateStep == eventCreateStepOptions && !m.eventCreateFocusSuggestions {
				switch m.eventCreateOptionsStep {
				case eventCreateOptAllDay:
					m.eventCreateAllDay = !m.eventCreateAllDay
				case eventCreateOptTeams:
					m.eventCreateTeams = !m.eventCreateTeams
				case eventCreateOptShowAs:
					m.cycleEventCreateShowAs()
				case eventCreateOptReminderOn:
					m.eventCreateReminderOn = !m.eventCreateReminderOn
				case eventCreateOptRecurrence:
					m.openEventCreateRecurrence()
				}
				return m, nil
			}
		}

		var cmd tea.Cmd
		switch m.eventCreateStep {
		case eventCreateStepSubject:
			m.eventCreateSubject, cmd = m.eventCreateSubject.Update(msg)
		case eventCreateStepAttendees:
			if m.eventCreateAttendeesStep == eventCreateAttendeeOptional {
				m.eventCreateOptionalAttendees, cmd = m.eventCreateOptionalAttendees.Update(msg)
			} else {
				m.eventCreateAttendees, cmd = m.eventCreateAttendees.Update(msg)
			}
			m.updateEventCreateFilteredSuggestions()
		case eventCreateStepStart:
			prev := m.eventCreateStart.Time()
			m.eventCreateStart, cmd = m.eventCreateStart.Update(msg)
			if syncCmd := m.eventCreateOnStartTimeChanged(prev); syncCmd != nil {
				cmds = append(cmds, syncCmd)
			}
		case eventCreateStepEnd:
			prev := m.eventCreateEnd.Time()
			m.eventCreateEnd, cmd = m.eventCreateEnd.Update(msg)
			if !m.eventCreateEnd.Time().Equal(prev) {
				cmds = append(cmds, m.scheduleDebouncedAvailabilityRefresh())
			}
		case eventCreateStepLocation:
			m.eventCreateLocation, cmd = m.eventCreateLocation.Update(msg)
		case eventCreateStepBody:
			m.eventCreateBody, cmd = m.eventCreateBody.Update(msg)
		case eventCreateStepOptions:
			if m.eventCreateOptionsStep == eventCreateOptReminderMin {
				m.eventCreateReminderMinInput, cmd = m.eventCreateReminderMinInput.Update(msg)
			}
		}
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m *mainModel) handleEventCreateRecurrenceUpdate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	patternTypes := []string{"daily", "weekly", "absoluteMonthly", "relativeMonthly", "absoluteYearly", "relativeYearly"}
	rangeTypes := []string{"noEnd", "endDate", "numbered"}
	weekDays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	indexes := []string{"first", "second", "third", "fourth", "last"}

	fields := m.recurrenceVisibleFields()
	if len(fields) == 0 {
		return m, nil
	}
	if m.eventCreateRecurrenceStep >= len(fields) {
		m.eventCreateRecurrenceStep = 0
	}
	cur := fields[m.eventCreateRecurrenceStep]

	switch msg.String() {
	case "esc":
		m.syncRecurrenceSettingsFromInputs()
		m.eventCreateRecurrence.Enabled = true
		m.eventCreateRecurrenceEnabled = true
		m.state = stateCalendarCreate
		m.eventCreateStep = eventCreateStepOptions
		m.eventCreateOptionsStep = eventCreateOptRecurrence
		m.updateEventCreateOptionsFocus()
		return m, nil
	case "backspace":
		if cur == rfInterval || cur == rfWeeklyDays || cur == rfDayOfMonth || cur == rfEndDate || cur == rfOccurrences {
			var cmd tea.Cmd
			switch cur {
			case rfInterval:
				m.eventCreateRecurrenceIntervalInput, cmd = m.eventCreateRecurrenceIntervalInput.Update(msg)
			case rfWeeklyDays:
				m.eventCreateRecurrenceDaysInput, cmd = m.eventCreateRecurrenceDaysInput.Update(msg)
			case rfDayOfMonth:
				m.eventCreateRecurrenceDayInput, cmd = m.eventCreateRecurrenceDayInput.Update(msg)
			case rfEndDate:
				m.eventCreateRecurrenceEndDateInput, cmd = m.eventCreateRecurrenceEndDateInput.Update(msg)
			case rfOccurrences:
				m.eventCreateRecurrenceCountInput, cmd = m.eventCreateRecurrenceCountInput.Update(msg)
			}
			return m, cmd
		}
		m.eventCreateRecurrenceEnabled = false
		m.eventCreateRecurrence = RecurrenceSettings{}
		m.state = stateCalendarCreate
		m.eventCreateStep = eventCreateStepOptions
		m.eventCreateOptionsStep = eventCreateOptRecurrence
		m.updateEventCreateOptionsFocus()
		return m, nil
	case "tab":
		m.syncRecurrenceSettingsFromInputs()
		m.eventCreateRecurrenceStep = (m.eventCreateRecurrenceStep + 1) % len(fields)
		m.updateEventCreateRecurrenceFocus()
		return m, nil
	case "shift+tab":
		m.syncRecurrenceSettingsFromInputs()
		m.eventCreateRecurrenceStep = (m.eventCreateRecurrenceStep - 1 + len(fields)) % len(fields)
		m.updateEventCreateRecurrenceFocus()
		return m, nil
	case " ":
		r := m.eventCreateRecurrence
		switch cur {
		case rfPattern:
			idx := 0
			for i, p := range patternTypes {
				if p == r.PatternType {
					idx = (i + 1) % len(patternTypes)
					break
				}
			}
			if r.PatternType == "" {
				r.PatternType = patternTypes[0]
			} else {
				r.PatternType = patternTypes[idx]
			}
		case rfIndex:
			idx := 0
			for i, v := range indexes {
				if v == r.Index {
					idx = (i + 1) % len(indexes)
					break
				}
			}
			if r.Index == "" {
				r.Index = indexes[0]
			} else {
				r.Index = indexes[idx]
			}
		case rfDayOfWeek:
			idx := 0
			for i, d := range weekDays {
				if d == r.DayOfWeek {
					idx = (i + 1) % len(weekDays)
					break
				}
			}
			if r.DayOfWeek == "" {
				r.DayOfWeek = weekDays[0]
			} else {
				r.DayOfWeek = weekDays[idx]
			}
		case rfRange:
			idx := 0
			for i, rt := range rangeTypes {
				if rt == r.RangeType {
					idx = (i + 1) % len(rangeTypes)
					break
				}
			}
			if r.RangeType == "" {
				r.RangeType = rangeTypes[0]
			} else {
				r.RangeType = rangeTypes[idx]
			}
			m.eventCreateRecurrence = r
			m.updateEventCreateRecurrenceFocus()
			return m, nil
		}
		m.eventCreateRecurrence = r
		return m, nil
	default:
		var cmd tea.Cmd
		switch cur {
		case rfInterval:
			m.eventCreateRecurrenceIntervalInput, cmd = m.eventCreateRecurrenceIntervalInput.Update(msg)
		case rfWeeklyDays:
			m.eventCreateRecurrenceDaysInput, cmd = m.eventCreateRecurrenceDaysInput.Update(msg)
		case rfDayOfMonth:
			m.eventCreateRecurrenceDayInput, cmd = m.eventCreateRecurrenceDayInput.Update(msg)
		case rfEndDate:
			m.eventCreateRecurrenceEndDateInput, cmd = m.eventCreateRecurrenceEndDateInput.Update(msg)
		case rfOccurrences:
			m.eventCreateRecurrenceCountInput, cmd = m.eventCreateRecurrenceCountInput.Update(msg)
		default:
			return m, nil
		}
		return m, cmd
	}
	return m, nil
}

func (m mainModel) eventCreateFieldSummary(step int) string {
	switch step {
	case eventCreateStepSubject:
		return m.eventCreateSubject.Value()
	case eventCreateStepAttendees:
		req := strings.TrimSpace(m.eventCreateAttendees.Value())
		opt := strings.TrimSpace(m.eventCreateOptionalAttendees.Value())
		if req == "" && opt == "" {
			return ""
		}
		var parts []string
		if req != "" {
			parts = append(parts, "req: "+req)
		}
		if opt != "" {
			parts = append(parts, "opt: "+opt)
		}
		return strings.Join(parts, " | ")
	case eventCreateStepStart:
		return m.eventCreateStart.Summary()
	case eventCreateStepEnd:
		return m.eventCreateEnd.Summary()
	case eventCreateStepLocation:
		return m.eventCreateLocation.Value()
	case eventCreateStepBody:
		body := strings.TrimSpace(m.eventCreateBody.Value())
		if body == "" {
			return ""
		}
		if idx := strings.Index(body, "\n"); idx >= 0 {
			body = body[:idx] + "..."
		}
		if len(body) > 40 {
			body = body[:38] + ".."
		}
		return body
	case eventCreateStepOptions:
		recStr := "Off"
		if m.eventCreateRecurrenceEnabled {
			recStr = RecurrencePreview(m.eventCreateRecurrenceForPreview())
		}
		return fmt.Sprintf("all-day=%v teams=%v show-as=%s reminder=%v(%dm) rec=%s",
			m.eventCreateAllDay, m.eventCreateTeams, m.eventCreateShowAs,
			m.eventCreateReminderOn, m.eventCreateReminderMin, recStr)
	}
	return ""
}

func truncateEventCreateSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return dimStyle.Render("(empty)")
	}
	if len(s) > max {
		return s[:max-2] + ".."
	}
	return s
}

func (m mainModel) renderCalendarCreateView() string {
	var s strings.Builder
	title := "📅  CREATE CALENDAR EVENT"
	if m.eventCreateIsEditing() {
		title = "📅  EDIT CALENDAR EVENT"
	}
	s.WriteString("   " + headerStyle.Render(title) + "\n\n")

	leftW := m.width * 45 / 100
	if leftW < 36 {
		leftW = 36
	}
	rightW := m.width - leftW - 6
	if rightW < 30 {
		rightW = 30
	}
	listHeight := m.height - 10
	if listHeight < 8 {
		listHeight = 8
	}

	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true)
	activeLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet)).Bold(true)
	inactiveLabel := dimStyle

	fieldLabels := []string{"Subject", "Attendees", "Start", "End", "Location", "Body", "Options"}
	var formLines []string
	summaryWidth := leftW - 6
	if summaryWidth < 10 {
		summaryWidth = 10
	}

	for i, label := range fieldLabels {
		var labelStr string
		if i == m.eventCreateStep {
			labelStr = activeLabel.Render("▸ " + label + ":")
		} else {
			labelStr = inactiveLabel.Render("  " + label + ":")
		}

		if i == m.eventCreateStep {
			var val string
			switch i {
			case eventCreateStepSubject:
				val = m.eventCreateSubject.View()
			case eventCreateStepAttendees:
				formLines = append(formLines, m.renderEventCreateAttendeeRows(activeLabel, inactiveLabel)...)
				if len(m.attendeeSuggestions) > 0 {
					formLines = append(formLines, m.renderAttendeeSuggestionsList(leftW-6))
				}
				continue
			case eventCreateStepStart:
				val = m.eventCreateStart.View()
			case eventCreateStepEnd:
				val = m.eventCreateEnd.View()
			case eventCreateStepLocation:
				val = m.eventCreateLocation.View()
			case eventCreateStepBody:
				val = m.eventCreateBody.View()
			case eventCreateStepOptions:
				formLines = append(formLines, m.renderEventCreateOptionsRows(activeLabel, inactiveLabel)...)
				continue
			}
			formLines = append(formLines, labelStr+"\n "+val)
		} else {
			formLines = append(formLines, labelStr+" "+truncateEventCreateSummary(m.eventCreateFieldSummary(i), summaryWidth))
		}
	}

	if m.eventCreateConflictCount > 0 {
		formLines = append(formLines, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(
			fmt.Sprintf("⚠ Conflict: %d attendee(s) busy during proposed time", m.eventCreateConflictCount),
		))
	}

	leftContent := strings.Join(formLines, "\n")
	leftStyle := paneNormalStyle
	if !m.eventCreateFocusSuggestions {
		leftStyle = paneActiveStyle
	}
	leftPane := leftStyle.Copy().Width(leftW).Height(listHeight).Render(leftContent)
	leftPane = applyPaneTitle(leftPane, "EVENT DETAILS", !m.eventCreateFocusSuggestions)

	var rightLines []string
	rightLines = append(rightLines, cyan.Render("BUSY TIMELINE")+" "+availabilityTimelineLegend())
	if m.eventCreateAvailLoading {
		rightLines = append(rightLines, m.spinner.View()+" Loading availability...")
	} else {
		rightLines = append(rightLines, m.renderBusyTimeline()...)
	}
	rightLines = append(rightLines, "")
	sugTitle := cyan.Render("SUGGESTED TIMES")
	if m.eventCreateFocusSuggestions {
		sugTitle += activeLabel.Render(" ◂ focused")
	} else {
		sugTitle += dimStyle.Render(" (Tab from Options)")
	}
	rightLines = append(rightLines, sugTitle)
	if len(m.eventCreateSuggestions) == 0 && !m.eventCreateAvailLoading {
		rightLines = append(rightLines, dimStyle.Render("  No suggestions (add attendees and set times)"))
	}
	maxSug := 8
	for i, sug := range m.eventCreateSuggestions {
		if i >= maxSug {
			break
		}
		line := fmt.Sprintf("  %s – %s (%s)",
			sug.Start.Local().Format("Mon 15:04"),
			sug.End.Local().Format("15:04"),
			formatMeetingConfidence(sug.Confidence),
		)
		if i == m.eventCreateSuggestionsSelected && m.eventCreateFocusSuggestions {
			line = selectedItemStyle.Render("▸ " + line)
		}
		rightLines = append(rightLines, line)
	}

	rightStyle := paneNormalStyle
	if m.eventCreateFocusSuggestions {
		rightStyle = paneActiveStyle
	}
	rightPane := rightStyle.Copy().Width(rightW).Height(listHeight).Render(strings.Join(rightLines, "\n"))
	rightPane = applyPaneTitle(rightPane, "AVAILABILITY", m.eventCreateFocusSuggestions)

	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane))
	return s.String()
}

func (m mainModel) renderBusyTimeline() []string {
	if len(m.eventCreateSchedules) == 0 {
		return []string{dimStyle.Render("  Add attendees to see busy times")}
	}
	startH := m.config.CalendarWorkStartHour
	if startH <= 0 {
		startH = 8
	}
	endH := m.config.CalendarWorkEndHour
	if endH <= 0 {
		endH = 18
	}
	slotsPerHour := 60 / eventCreateAvailIntervalMin
	if slotsPerHour <= 0 {
		slotsPerHour = 2
	}
	slotCount := (endH - startH) * slotsPerHour
	if slotCount <= 0 {
		slotCount = 20
	}
	hourColWidth := slotsPerHour * eventCreateSlotCharWidth

	const emailColWidth = 11

	var lines []string
	header := strings.Repeat(" ", emailColWidth)
	for h := startH; h < endH; h++ {
		header += fmt.Sprintf("%-*d", hourColWidth, h)
	}
	lines = append(lines, dimStyle.Render(header))

	eventStart, eventEnd, _ := m.parsedEventCreateTimes()

	for _, sch := range m.eventCreateSchedules {
		email := sch.ScheduleID
		if len(email) > 10 {
			email = email[:8] + ".."
		}
		row := fmt.Sprintf("%-10s ", email)
		view := sch.AvailabilityView
		for i := 0; i < slotCount && i < len(view); i++ {
			slotStart := m.eventCreateScheduleQueryStart.Add(time.Duration(i) * eventCreateAvailIntervalMin * time.Minute)
			slotEnd := slotStart.Add(eventCreateAvailIntervalMin * time.Minute)
			inEvent := !eventEnd.IsZero() && slotEnd.After(eventStart) && slotStart.Before(eventEnd)
			row += formatAvailabilityCell(view[i], inEvent, eventCreateSlotCharWidth)
		}
		lines = append(lines, row)
	}
	return lines
}

func formatAvailabilityCell(code byte, inEvent bool, width int) string {
	sym := AvailabilitySymbol(code)
	content := sym
	if vis := lipgloss.Width(sym); vis < width {
		content += strings.Repeat(" ", width-vis)
	}

	if inEvent {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(ColorGreen)).
			Foreground(lipgloss.Color(ColorBg)).
			Bold(true).
			Render(content)
	}

	switch code {
	case '0':
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(sym) + strings.Repeat(" ", width-lipgloss.Width(sym))
	case '1':
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow)).Render(sym) + strings.Repeat(" ", width-lipgloss.Width(sym))
	case '2', '3':
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render(sym) + strings.Repeat(" ", width-lipgloss.Width(sym))
	case '4':
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Render(sym) + strings.Repeat(" ", width-lipgloss.Width(sym))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(sym) + strings.Repeat(" ", width-lipgloss.Width(sym))
	}
}

func availabilityTimelineLegend() string {
	proposedSample := lipgloss.NewStyle().
		Background(lipgloss.Color(ColorGreen)).
		Foreground(lipgloss.Color(ColorBg)).
		Bold(true).
		Render(" ## ")
	parts := []string{
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtext)).Render(". free"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow)).Render("~ tentative"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render("# busy"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render("! OOF"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Render("W elsewhere"),
		proposedSample + dimStyle.Render("proposed time"),
	}
	return dimStyle.Render("(") + strings.Join(parts, dimStyle.Render("  ")) + dimStyle.Render(")")
}

func (m mainModel) renderEventCreateAttendeeRows(activeLabel, inactiveLabel lipgloss.Style) []string {
	rows := []struct {
		step  int
		label string
		input textinput.Model
	}{
		{eventCreateAttendeeRequired, "Required", m.eventCreateAttendees},
		{eventCreateAttendeeOptional, "Optional", m.eventCreateOptionalAttendees},
	}
	var lines []string
	lines = append(lines, activeLabel.Render("▸ Attendees:"))
	for _, row := range rows {
		marker := "  "
		lbl := inactiveLabel
		if row.step == m.eventCreateAttendeesStep {
			marker = "  ▸ "
			lbl = activeLabel
		}
		var val string
		if row.step == m.eventCreateAttendeesStep {
			val = row.input.View()
		} else {
			val = row.input.Value()
			if strings.TrimSpace(val) == "" {
				val = dimStyle.Render("(empty)")
			}
		}
		lines = append(lines, marker+lbl.Render(row.label+":")+" "+val)
	}
	lines = append(lines, dimStyle.Render("  Tab/Shift+Tab move between required and optional"))
	return lines
}

func (m mainModel) renderEventCreateOptionsRows(activeLabel, inactiveLabel lipgloss.Style) []string {
	onOff := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	recStr := "Off (Space or R to configure)"
	if m.eventCreateRecurrenceEnabled {
		recStr = RecurrencePreview(m.eventCreateRecurrenceForPreview())
	}
	rows := []struct {
		step  int
		label string
		value string
		hint  string
	}{
		{eventCreateOptAllDay, "All-day event", onOff(m.eventCreateAllDay), "Space toggle"},
		{eventCreateOptTeams, "Teams meeting", onOff(m.eventCreateTeams), "Space toggle"},
		{eventCreateOptShowAs, "Show as", m.eventCreateShowAs, "Space cycle"},
		{eventCreateOptReminderOn, "Reminder", onOff(m.eventCreateReminderOn), "Space toggle"},
		{eventCreateOptReminderMin, "Reminder (min)", "", "type minutes"},
		{eventCreateOptRecurrence, "Recurrence", recStr, "Space or R"},
	}
	var lines []string
	lines = append(lines, activeLabel.Render("▸ Options:"))
	for _, row := range rows {
		marker := "  "
		lbl := inactiveLabel
		if row.step == m.eventCreateOptionsStep {
			marker = "  ▸ "
			lbl = activeLabel
		}
		val := row.value
		if row.step == eventCreateOptReminderMin {
			if row.step == m.eventCreateOptionsStep {
				val = m.eventCreateReminderMinInput.View()
			} else {
				val = m.eventCreateReminderMinInput.Value() + " min"
			}
		}
		line := marker + lbl.Render(row.label+":") + " " + val
		if row.step == m.eventCreateOptionsStep && row.hint != "" {
			line += dimStyle.Render("  [" + row.hint + "]")
		}
		lines = append(lines, line)
	}
	lines = append(lines, dimStyle.Render("  Tab/Shift+Tab move between options"))
	return lines
}

func (m mainModel) renderCalendarRecurrencePopup(width int) string {
	r := m.eventCreateRecurrence
	if r.PatternType == "" {
		r.PatternType = "weekly"
	}
	if r.RangeType == "" {
		r.RangeType = "noEnd"
	}
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true)
	active := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorViolet)).Bold(true)

	fields := m.recurrenceVisibleFields()
	if m.eventCreateRecurrenceStep >= len(fields) {
		m.eventCreateRecurrenceStep = 0
	}

	var lines []string
	lines = append(lines, headerStyle.Render("RECURRENCE PATTERN"))
	lines = append(lines, "")

	fieldLabel := func(kind recurFieldKind) string {
		switch kind {
		case rfPattern:
			return "Pattern"
		case rfInterval:
			return "Every (interval)"
		case rfWeeklyDays:
			return "Days of week"
		case rfDayOfMonth:
			return "Day of month"
		case rfIndex:
			return "Week of month"
		case rfDayOfWeek:
			return "Day of week"
		case rfRange:
			return "Range"
		case rfEndDate:
			return "End date"
		case rfOccurrences:
			return "Occurrences"
		default:
			return ""
		}
	}

	for i, kind := range fields {
		marker := "  "
		lblStyle := cyan
		if i == m.eventCreateRecurrenceStep {
			marker = "▸ "
			lblStyle = active
		}
		var val string
		var hint string
		switch kind {
		case rfPattern:
			val = r.PatternType
			hint = "Space cycle"
		case rfInterval:
			if i == m.eventCreateRecurrenceStep {
				val = m.eventCreateRecurrenceIntervalInput.View()
			} else {
				val = m.eventCreateRecurrenceIntervalInput.Value()
			}
			hint = "type number"
		case rfWeeklyDays:
			if i == m.eventCreateRecurrenceStep {
				val = m.eventCreateRecurrenceDaysInput.View()
			} else {
				val = m.eventCreateRecurrenceDaysInput.Value()
			}
			hint = "mon,wed,fri"
		case rfDayOfMonth:
			if i == m.eventCreateRecurrenceStep {
				val = m.eventCreateRecurrenceDayInput.View()
			} else {
				val = m.eventCreateRecurrenceDayInput.Value()
			}
			hint = "1-31"
		case rfIndex:
			val = r.Index
			if val == "" {
				val = "first"
			}
			hint = "Space cycle"
		case rfDayOfWeek:
			val = r.DayOfWeek
			if val == "" {
				val = "monday"
			}
			hint = "Space cycle"
		case rfRange:
			val = r.RangeType
			hint = "Space cycle"
		case rfEndDate:
			if i == m.eventCreateRecurrenceStep {
				val = m.eventCreateRecurrenceEndDateInput.View()
			} else {
				val = m.eventCreateRecurrenceEndDateInput.Value()
			}
			hint = "YYYY-MM-DD"
		case rfOccurrences:
			if i == m.eventCreateRecurrenceStep {
				val = m.eventCreateRecurrenceCountInput.View()
			} else {
				val = m.eventCreateRecurrenceCountInput.Value()
			}
			hint = "type number"
		}
		line := marker + lblStyle.Render(fieldLabel(kind)+":") + " " + val
		if i == m.eventCreateRecurrenceStep && hint != "" {
			line += dimStyle.Render("  [" + hint + "]")
		}
		lines = append(lines, line)
	}

	lines = append(lines, "", dimStyle.Render("Preview: "+RecurrencePreview(m.eventCreateRecurrenceForPreview())))
	lines = append(lines, "", dimStyle.Render("[Tab] fields | [Space] cycle enums | type numbers/dates | [Esc] save | [Backspace] clear"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorViolet)).
		Padding(1, 2).
		Width(width).
		Render(strings.Join(lines, "\n"))
	return box
}

func (m mainModel) renderCalendarCreateCancelConfirmPopup(width int) string {
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	prompt := "Discard unsaved event?"
	detail := "All entered event details will be lost."
	if m.eventCreateIsEditing() {
		prompt = "Discard unsaved changes?"
		detail = "All edits to this event will be lost."
	}
	content := yellow.Render(prompt) + "\n\n" +
		dimStyle.Render(detail) + "\n\n" +
		dimStyle.Render("[y] Yes, discard  |  [n/Esc] No, continue editing")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("3")).
		Padding(1, 2).
		Width(width).
		Render(content)
}
