package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mmcdole/gofeed"
	tele "gopkg.in/telebot.v4"
)

func titleOrDefault(t sql.NullString, def string) string {
	if t.Valid && t.String != "" {
		return t.String
	}
	return def
}

func handleRemoveCmd(c tele.Context, id int64) error {
	var t sql.NullString
	db.QueryRow("SELECT title FROM subscriptions WHERE id = $1", id).Scan(&t)
	if _, err := db.Exec("DELETE FROM subscriptions WHERE id = $1", id); err != nil {
		return c.Send("Failed to remove.")
	}
	return c.Send(fmt.Sprintf("✅ Removed %s", titleOrDefault(t, "subscription")))
}

func handleRefreshCmd(c tele.Context, id int64) error {
	var t sql.NullString
	var iv int64
	if err := db.QueryRow("SELECT title, refresh_interval FROM subscriptions WHERE id = $1", id).Scan(&t, &iv); err != nil {
		return c.Send("Subscription not found.")
	}
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data("10m", fmt.Sprintf("setrefresh:%d:600", id)), m.Data("30m", fmt.Sprintf("setrefresh:%d:1800", id)), m.Data("1h", fmt.Sprintf("setrefresh:%d:3600", id))),
		m.Row(m.Data("6h", fmt.Sprintf("setrefresh:%d:21600", id)), m.Data("1d", fmt.Sprintf("setrefresh:%d:86400", id)), m.Data("1w", fmt.Sprintf("setrefresh:%d:604800", id))),
	)
	return c.Send(fmt.Sprintf("⏱ Set refresh interval for <b>%s</b>\n\nCurrently set to: %s", titleOrDefault(t, "this feed"), interval(iv)), tele.ModeHTML, m)
}

func handlePauseCmd(c tele.Context, id int64) error {
	var paused bool
	var t sql.NullString
	db.QueryRow("SELECT paused, title FROM subscriptions WHERE id = $1", id).Scan(&paused, &t)
	if _, err := db.Exec("UPDATE subscriptions SET paused = $1 WHERE id = $2", !paused, id); err != nil {
		return c.Send("Failed to update.")
	}
	if !paused {
		return c.Send(fmt.Sprintf("⏸️ Paused %s", titleOrDefault(t, "subscription")))
	}
	return c.Send(fmt.Sprintf("▶️ Resumed %s", titleOrDefault(t, "subscription")))
}

func handleLatestCmd(c tele.Context, id int64) error {
	var u string
	if err := db.QueryRow("SELECT feed_url FROM subscriptions WHERE id = $1", id).Scan(&u); err != nil {
		return c.Send("Subscription not found.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f, err := gofeed.NewParser().ParseURLWithContext(u, ctx)
	if err != nil {
		return c.Send("Failed to fetch feed.")
	}
	if len(f.Items) == 0 {
		return c.Send("No items in feed.")
	}
	item := f.Items[0]
	msg := formatItem(f.Title, item)
	return c.Send(msg, tele.ModeHTML)
}
