package main

import "testing"

func TestPlayLinuxNotificationSoundSkipsWhenDisabledOrEmpty(t *testing.T) {
	// These must return immediately and never invoke paplay.
	playLinuxNotificationSound(false, defaultNotificationSoundFile)
	playLinuxNotificationSound(true, "")
	playLinuxNotificationSound(true, "   ")
}
