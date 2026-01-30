package main

import (
	"database/sql"
	"fmt"
	"strings"

	tele "gopkg.in/telebot.v4"
)

func interval(seconds int64) string {
	switch {
	case seconds >= 86400:
		return fmt.Sprintf("%dd", seconds/86400)
	case seconds >= 3600:
		return fmt.Sprintf("%dh", seconds/3600)
	default:
		return fmt.Sprintf("%dm", seconds/60)
	}
}

func buildList(userID int64) (string, error) {
	rows, err := db.Query("SELECT id, feed_url, title, refresh_interval, last_refreshed, paused FROM subscriptions WHERE user_id = $1 ORDER BY created_at", userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var entries []string
	for rows.Next() {
		var id int64
		var u, t sql.NullString
		var refreshInterval sql.NullInt64
		var lastRefreshed sql.NullTime
		var paused bool
		if err := rows.Scan(&id, &u, &t, &refreshInterval, &lastRefreshed, &paused); err != nil {
			continue
		}
		name := u.String
		if t.Valid && t.String != "" {
			name = t.String
		}
		status := "🟢"
		pauseLabel := "Pause"
		if paused {
			status = "⏸️"
			pauseLabel = "Resume"
		}

		entry := fmt.Sprintf("%s <b>%s</b>\n%s\nRefresh: %s", status, name, u.String, interval(refreshInterval.Int64))
		if lastRefreshed.Valid {
			entry += fmt.Sprintf(" | Last: %s", lastRefreshed.Time.Format("Jan 2 15:04"))
		}

		links := fmt.Sprintf("└ <a href=\"https://t.me/%s?start=rm_%d\">Remove</a> • <a href=\"https://t.me/%s?start=rf_%d\">Refresh</a> • <a href=\"https://t.me/%s?start=ps_%d\">%s</a> • <a href=\"https://t.me/%s?start=lt_%d\">Latest</a>",
			botUsername, id, botUsername, id, botUsername, id, pauseLabel, botUsername, id)

		entries = append(entries, entry+"\n"+links)
	}
	if len(entries) == 0 {
		return "", nil
	}
	return fmt.Sprintf("📋 <b>Your Subscriptions</b>\n\n%s", strings.Join(entries, "\n\n")), nil
}

func listKeyboard() *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{}
	menu.Inline(menu.Row(menu.Data("🔄 Refresh", "refresh_list")))
	return menu
}
