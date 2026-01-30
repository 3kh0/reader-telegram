package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	tele "gopkg.in/telebot.v4"
)

func handleStart(c tele.Context) error {
	payload := c.Message().Payload
	if payload == "" {
		return c.Send("Hello! Send me an RSS feed URL to subscribe, or use /list to see your feeds!")
	}

	parts := strings.SplitN(payload, "_", 2)
	if len(parts) != 2 {
		return c.Send("Don't know what to do with that! But hey, send me an RSS feed URL and I can subscribe you to it!")
	}
	action, idStr := parts[0], parts[1]

	var subID int64
	if _, err := fmt.Sscanf(idStr, "%d", &subID); err != nil {
		return c.Send("bad id")
	}

	var userID int64
	if err := db.QueryRow("SELECT user_id FROM subscriptions WHERE id = $1", subID).Scan(&userID); err != nil {
		return c.Send("❌ Subscription not found.")
	}
	if userID != c.Sender().ID {
		return c.Send("❌ Subscription not found.")
	}

	switch action {
	case "rm":
		return handleRemoveCmd(c, subID)
	case "rf":
		return handleRefreshCmd(c, subID)
	case "ps":
		return handlePauseCmd(c, subID)
	case "lt":
		return handleLatestCmd(c, subID)
	default:
		return c.Send("Unknown action.")
	}
}

func handleList(c tele.Context) error {
	content, err := buildList(c.Sender().ID)
	if err != nil {
		log.Printf("cant fetch subs %v", err)
		return c.Send("cant fetch subs")
	}
	if content == "" {
		return c.Send("❌ No subscriptions yet. Send me an RSS feed URL to subscribe.")
	}
	return c.Send(content, tele.ModeHTML, tele.NoPreview, listKeyboard())
}

func handleText(c tele.Context) error {
	t := strings.TrimSpace(c.Text())
	lines := strings.Fields(t)
	var urls []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if isValidURL(line) {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		return c.Send("Don't know what to do with that! But hey, send me an RSS feed URL and I can subscribe you to it!")
	}
	return addSubs(c, urls)
}

func handleInspect(c tele.Context) error {
	payload := c.Message().Payload
	if !isValidURL(payload) {
		return c.Send("❌ Please provide a valid RSS feed URL")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f, err := gofeed.NewParser().ParseURLWithContext(payload, ctx)
	if err != nil {
		return c.Send("❌ Failed to parse the RSS feed, we got:\n<blockquote><code>" + err.Error() + "</code></blockquote>", tele.ModeHTML)
	}

	var items []string
	for i, item := range f.Items {
		if i >= 5 {
			break
		}
		items = append(items, fmt.Sprintf("• <a href=\"%s\">%s</a> - %s", item.Link, esc(item.Title), relativeTime(*item.PublishedParsed)))
	}

	msg := fmt.Sprintf(
		"Inspecting: <code>%s</code>\nDescription: <code>%s</code>\nURL: <code>%s</code>\nFeed type: <code>%s</code>\nFeed version: <code>%s</code>\n\nItems:\n\n%s",
		esc(f.Title),
		esc(f.Description),
		esc(payload),
		esc(f.FeedType),
		esc(f.FeedVersion),
		strings.Join(items, "\n"),
	)

	if err := c.Send(msg, tele.ModeHTML); err != nil {
		return err
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil
	}

	doc := &tele.Document{
		File:     tele.FromReader(strings.NewReader(string(data))),
		FileName: fmt.Sprintf("%s_feed.json", f.Title),
		Caption:  "full data dump:",
	}
	return c.Send(doc)
}

func snoozeAll(userID int64, pause bool) (int64, error) {
	res, err := db.Exec("UPDATE subscriptions SET paused = $1 WHERE user_id = $2", pause, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func handleSnooze(c tele.Context) error {
	payload := c.Message().Payload
	pause := payload != "resume"

	n, err := snoozeAll(c.Sender().ID, pause)
	if err != nil {
		log.Printf("cant update pause state: %v", err)
		return c.Send("cant update pause state")
	}
	if n == 0 {
		return c.Send("❌ You have no subscriptions to " + map[bool]string{true: "pause", false: "resume"}[pause] + ".")
	}
	action := map[bool]string{true: "resume", false: "pause"}[pause]
	btn := tele.InlineButton{Text: "↩️ Undo (/snooze " + action + ")", Data: "snooze_" + action}
	return c.Send(fmt.Sprintf("✅ Successfully %sd all your %d subscriptions.", map[bool]string{true: "pause", false: "resume"}[pause], n), &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{btn}}})
}

func esc(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}

func callback(c tele.Context) error {
	data := strings.TrimPrefix(c.Callback().Data, "\f")
	if data == "snooze_pause" || data == "snooze_resume" {
		pause := data == "snooze_pause"
		n, err := snoozeAll(c.Sender().ID, pause)
		if err != nil {
			return c.Respond(&tele.CallbackResponse{Text: "Error: " + err.Error()})
		}
		c.Edit(fmt.Sprintf("✅ Successfully %sd all your %d subscriptions.", map[bool]string{true: "pause", false: "resume"}[pause], n))
		return c.Respond()
	}
	if data == "refresh_list" {
		content, err := buildList(c.Sender().ID)
		if err != nil {
			return c.Respond(&tele.CallbackResponse{Text: "cant fetch"})
		}
		if content == "" {
			c.Edit("❌ No subscriptions yet! Send me an RSS feed URL to add one.")
			return c.Respond()
		}
		c.Edit(content, tele.ModeHTML, tele.NoPreview, listKeyboard())
		return c.Respond(&tele.CallbackResponse{Text: "Refreshed!"})
	}

	parts := strings.SplitN(data, ":", 3)
	if len(parts) < 3 {
		return c.Respond(&tele.CallbackResponse{Text: "invalid"})
	}

	var subID, iv int64
	if _, err := fmt.Sscanf(parts[1], "%d", &subID); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "invalid id"})
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &iv); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "invalid interval"})
	}

	var userID int64
	if err := db.QueryRow("SELECT user_id FROM subscriptions WHERE id = $1", subID).Scan(&userID); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "not found"})
	}
	if userID != c.Sender().ID {
		return c.Respond(&tele.CallbackResponse{Text: "not yours"})
	}

	if _, err := db.Exec("UPDATE subscriptions SET refresh_interval = $1 WHERE id = $2", iv, subID); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "cant update"})
	}
	c.Edit(fmt.Sprintf("✅ Updated to %s", interval(iv)))
	return c.Respond(&tele.CallbackResponse{Text: fmt.Sprintf("✅ Set to %s", interval(iv))})
}

func isValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func addSubs(c tele.Context, urls []string) error {
	total := len(urls)
	msg, _ := c.Bot().Send(c.Recipient(), fmt.Sprintf("⏳ Checking 1/%d...", total))
	edit := func(s string) {
		if msg != nil {
			c.Bot().Edit(msg, s)
		}
	}

	parser := gofeed.NewParser()
	var added []string
	var failed []string

	for i, u := range urls {
		edit(fmt.Sprintf("⏳ Checking %d/%d...", i+1, total))

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		f, err := parser.ParseURLWithContext(u, ctx)
		cancel()

		if err != nil {
			log.Printf("cant parse %s: %v", u, err)
			failed = append(failed, u)
			continue
		}
		if _, err = db.Exec("INSERT INTO subscriptions (user_id, feed_url, title) VALUES ($1, $2, $3) ON CONFLICT (user_id, feed_url) DO UPDATE SET title = $3", c.Sender().ID, u, f.Title); err != nil {
			log.Printf("cant save %v", err)
			failed = append(failed, u)
			continue
		}
		added = append(added, f.Title)
	}

	if len(added) == 0 {
		edit("❌ Failed to add any feeds:\n" + strings.Join(failed, "\n"))
		return nil
	}

	var result string
	if len(added) == 1 {
		result = fmt.Sprintf("✅ Added %s", added[0])
	} else {
		result = fmt.Sprintf("✅ Added %d feeds: %s", len(added), strings.Join(added, ", "))
	}
	if len(failed) > 0 {
		result += fmt.Sprintf("\n❌ %d failed:\n%s", len(failed), strings.Join(failed, "\n"))
	}
	edit(result)
	return nil
}
