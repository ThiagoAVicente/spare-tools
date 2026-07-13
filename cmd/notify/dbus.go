package main

import (
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/ThiagoAVicente/spare-tools/internal/comum"
)

// Notification urgency levels per the freedesktop.org notification spec.
const (
	urgencyNormal   = byte(1)
	urgencyCritical = byte(2)
)

// Expiry timeouts: -1 lets the server pick a default, 0 keeps the
// notification on screen until dismissed.
const (
	expireDefault = int32(-1)
	expireNever   = int32(0)
)

// sendNotification fires a desktop notification over the D-Bus session bus
// describing the outcome of the child command.
func sendNotification(opts options, cmdName string, code int, dur time.Duration) error {
	summary, body, urgency, expire := buildNotification(opts, cmdName, code, dur)

	hints := map[string]dbus.Variant{
		"urgency": dbus.MakeVariant(urgency),
	}
	if opts.sound {
		hints["sound-name"] = dbus.MakeVariant("message-new-instant")
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		"notify",   // app_name
		uint32(0),  // replaces_id
		"",         // app_icon
		summary,    // summary
		body,       // body
		[]string{}, // actions
		hints,      // hints
		expire,     // expire_timeout
	)
	return call.Err
}

// buildNotification composes summary/body and picks urgency and expiry
// based on the child's exit code.
func buildNotification(opts options, cmdName string, code int, dur time.Duration) (summary, body string, urgency byte, expire int32) {
	var result string
	if code == comum.ExitOK {
		result = fmt.Sprintf("✓ %s (%s)", cmdName, comum.FormatDuration(dur))
		urgency = urgencyNormal
		expire = expireDefault
	} else {
		result = fmt.Sprintf("✗ %s failed (exit %d, %s)", cmdName, code, comum.FormatDuration(dur))
		urgency = urgencyCritical
		expire = expireNever
	}
	if opts.title != "" {
		return opts.title, result, urgency, expire
	}
	return result, "", urgency, expire
}
